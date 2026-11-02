package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"

	"go.yaml.in/yaml/v3"
)

type report struct {
	Success     int       `json:"success" yaml:"success"`
	Fail        int       `json:"fail" yaml:"fail"`
	SuccessRate float64   `json:"success_rate" yaml:"success_rate"`
	AverageTTFB int64     `json:"average_ttfb" yaml:"average_ttfb"`
	Results     []*result `json:"results" yaml:"results"`
}

func newReport(results []*result) *report {
	rep := &report{Results: results}
	var ttfb int64
	for _, res := range results {
		if !res.OK {
			rep.Fail++
			continue
		}
		rep.Success++
		ttfb += res.Latency.TTFB
	}
	if rep.Success > 0 {
		rep.AverageTTFB = ttfb / int64(rep.Success)
	}
	if len(results) > 0 {
		rep.SuccessRate = float64(rep.Success) * 100 / float64(len(results))
	}
	return rep
}

func writeReport(w io.Writer, format string, rep *report) error {
	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	case "yaml":
		enc := yaml.NewEncoder(w)
		if err := enc.Encode(rep); err != nil {
			return err
		}
		return enc.Close()
	}

	for _, res := range rep.Results {
		if !res.OK {
			fmt.Fprintf(w, "FAIL %s: %s\n", res.Proxy, res.Error)
		}
	}
	fmt.Fprintf(w, "Success rate:      %d/%d (%.1f%%)\n", rep.Success, len(rep.Results), rep.SuccessRate)
	_, err := fmt.Fprintf(w, "Average TTFB:      %dms\n", rep.AverageTTFB)
	return err
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
