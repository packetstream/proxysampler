package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"

	"go.yaml.in/yaml/v3"
)

type report struct {
	Success     int            `json:"success" yaml:"success"`
	Fail        int            `json:"fail" yaml:"fail"`
	SuccessRate float64        `json:"success_rate" yaml:"success_rate"`
	AverageTTFB int64          `json:"average_ttfb" yaml:"average_ttfb"`
	TTFB        *stats         `json:"ttfb,omitempty" yaml:"ttfb,omitempty"`
	Total       *stats         `json:"total,omitempty" yaml:"total,omitempty"`
	StatusCodes map[int]int    `json:"status_codes,omitempty" yaml:"status_codes,omitempty"`
	Errors      map[string]int `json:"errors,omitempty" yaml:"errors,omitempty"`
	Exits       *exitSummary   `json:"exits,omitempty" yaml:"exits,omitempty"`
	Results     []*result      `json:"results,omitempty" yaml:"results,omitempty"`
}

// stats summarises the latencies of successful requests, in milliseconds.
type stats struct {
	Avg int64 `json:"avg" yaml:"avg"`
	P50 int64 `json:"p50" yaml:"p50"`
	P90 int64 `json:"p90" yaml:"p90"`
	P99 int64 `json:"p99" yaml:"p99"`
	Max int64 `json:"max" yaml:"max"`
}

// exitSummary describes the exit IPs seen with --check-exit.
type exitSummary struct {
	UniqueIPs int            `json:"unique_ips" yaml:"unique_ips"`
	Countries map[string]int `json:"countries" yaml:"countries"`
	// StickySessions counts proxies with a _session- option that made more
	// than one successful request, and SessionsChangedIP those that exited
	// from more than one IP.
	StickySessions    int `json:"sticky_sessions" yaml:"sticky_sessions"`
	SessionsChangedIP int `json:"sessions_changed_ip" yaml:"sessions_changed_ip"`
}

func newReport(results []*result) *report {
	rep := &report{Results: results, StatusCodes: map[int]int{}, Errors: map[string]int{}}
	var ttfb, total []int64
	for _, res := range results {
		if res.StatusCode != 0 {
			rep.StatusCodes[res.StatusCode]++
		}
		if !res.OK {
			rep.Fail++
			rep.Errors[res.Error]++
			continue
		}
		rep.Success++
		ttfb = append(ttfb, res.Latency.TTFB)
		total = append(total, res.Latency.Total)
	}
	if rep.Success > 0 {
		rep.TTFB, rep.Total = newStats(ttfb), newStats(total)
		rep.AverageTTFB = rep.TTFB.Avg
	}
	if len(results) > 0 {
		rep.SuccessRate = float64(rep.Success) * 100 / float64(len(results))
	}
	rep.Exits = summariseExits(results)
	return rep
}

func newStats(values []int64) *stats {
	slices.Sort(values)
	var sum int64
	for _, v := range values {
		sum += v
	}
	// Nearest-rank percentile.
	percentile := func(p int) int64 {
		return values[max(0, (p*len(values)+99)/100-1)]
	}
	return &stats{
		Avg: sum / int64(len(values)),
		P50: percentile(50),
		P90: percentile(90),
		P99: percentile(99),
		Max: values[len(values)-1],
	}
}

func summariseExits(results []*result) *exitSummary {
	type session struct {
		ips map[string]bool
		ok  int
	}
	summary := &exitSummary{Countries: map[string]int{}}
	ips := map[string]bool{}
	sessions := map[string]*session{}
	for _, res := range results {
		if res.Exit == nil || !res.OK {
			continue
		}
		ips[res.Exit.IP] = true
		summary.Countries[res.Exit.Country]++
		if res.sticky {
			if sessions[res.Proxy] == nil {
				sessions[res.Proxy] = &session{ips: map[string]bool{}}
			}
			sessions[res.Proxy].ips[res.Exit.IP] = true
			sessions[res.Proxy].ok++
		}
	}
	if len(ips) == 0 {
		return nil
	}
	summary.UniqueIPs = len(ips)
	for _, s := range sessions {
		if s.ok > 1 {
			summary.StickySessions++
			if len(s.ips) > 1 {
				summary.SessionsChangedIP++
			}
		}
	}
	return summary
}

func writeReport(w io.Writer, format string, rep *report) error {
	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	case "jsonl":
		// Results were streamed already.
		summary := *rep
		summary.Results = nil
		return json.NewEncoder(w).Encode(struct {
			Type string `json:"type"`
			*report
		}{"summary", &summary})
	case "yaml":
		enc := yaml.NewEncoder(w)
		if err := enc.Encode(rep); err != nil {
			return err
		}
		return enc.Close()
	}

	fmt.Fprintf(w, "Success rate:      %d/%d (%.1f%%)\n", rep.Success, len(rep.Results), rep.SuccessRate)
	if rep.TTFB != nil {
		fmt.Fprintf(w, "TTFB:              %s\n", rep.TTFB)
		fmt.Fprintf(w, "Total time:        %s\n", rep.Total)
	}
	if len(rep.StatusCodes) > 0 {
		fmt.Fprintf(w, "Status codes:      %s\n", countList(rep.StatusCodes, strconv.Itoa))
	}
	if rep.Exits != nil {
		fmt.Fprintf(w, "Exit IPs:          %d unique\n", rep.Exits.UniqueIPs)
		fmt.Fprintf(w, "Exit countries:    %s\n", countList(rep.Exits.Countries, func(c string) string { return c }))
		if rep.Exits.StickySessions > 0 {
			fmt.Fprintf(w, "Sticky sessions:   %d/%d kept one exit IP\n",
				rep.Exits.StickySessions-rep.Exits.SessionsChangedIP, rep.Exits.StickySessions)
		}
	}

	// Group failures by proxy and error.
	failures := map[string]int{}
	for _, res := range rep.Results {
		if !res.OK {
			failures[res.Proxy+": "+res.Error]++
		}
	}
	if len(failures) > 0 {
		fmt.Fprintln(w, "Failures:")
		for _, line := range slices.Sorted(maps.Keys(failures)) {
			fmt.Fprintf(w, "  %d× %s\n", failures[line], line)
		}
	}
	return nil
}

func (s *stats) String() string {
	return fmt.Sprintf("avg %dms  p50 %dms  p90 %dms  p99 %dms  max %dms", s.Avg, s.P50, s.P90, s.P99, s.Max)
}

// countList formats counts as "key×n, key×n", most frequent first.
func countList[K cmp.Ordered](counts map[K]int, format func(K) string) string {
	keys := slices.SortedFunc(maps.Keys(counts), func(a, b K) int {
		return cmp.Or(counts[b]-counts[a], cmp.Compare(a, b))
	})
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf("%s×%d", format(k), counts[k])
	}
	return strings.Join(parts, ", ")
}

// streamResult returns a function that writes each result as a JSON line.
func streamResult(w io.Writer) func(*result) {
	enc := json.NewEncoder(w)
	return func(res *result) {
		_ = enc.Encode(struct {
			Type string `json:"type"`
			*result
		}{"result", res})
	}
}

// progress shows a count of finished requests on stderr while a plaintext
// report runs in a terminal.
type progress struct {
	mu       sync.Mutex
	w        io.Writer
	finished int
	total    int
}

func newProgress(cfg config, stderr io.Writer, total int) *progress {
	if cfg.output != "plaintext" || !isTerminal(stderr) {
		return &progress{total: total}
	}
	return &progress{w: stderr, total: total}
}

func (p *progress) done() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.finished++
	if p.w != nil {
		fmt.Fprintf(p.w, "\rTested %d/%d", p.finished, p.total)
	}
}

func (p *progress) finish() {
	if p.w != nil {
		fmt.Fprint(p.w, "\r\033[K")
	}
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
