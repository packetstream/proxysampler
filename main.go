// Command proxysampler tests proxies and reports their success rate and
// response times.
package main

import (
	"crypto/x509"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Exit codes.
const (
	exitOK       = 0
	exitBelowMin = 1
	exitUsage    = 2
)

// maxRequests limits the number of requests in one run.
const maxRequests = 1_000_000

type config struct {
	output              string
	includeResponseBody bool
	proxyFile           string
	singleProxy         string
	endpoint            string
	maxThreads          int
	delay               time.Duration
	timeout             time.Duration
	expectStatus        statusSet
	minSuccess          float64
	count               int
	checkExit           bool

	// PacketStream proxy options, used when neither --proxy nor --file is set.
	protocol string
	country  string
	session  string
	host     string

	// rootCAs replaces the system roots in tests.
	rootCAs *x509.CertPool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	cfg, err := parseFlags(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return exitOK
	}
	if err != nil {
		fmt.Fprintf(stderr, "proxysampler: %v\nRun proxysampler --help for usage.\n", err)
		return exitUsage
	}
	return execute(cfg, stdout, stderr)
}

func execute(cfg config, stdout, stderr io.Writer) int {
	proxies, err := loadProxies(cfg)
	if err != nil {
		fmt.Fprintf(stderr, "proxysampler: %v\n", err)
		return exitUsage
	}
	if len(proxies)*cfg.count > maxRequests {
		fmt.Fprintf(stderr, "proxysampler: %d proxies with --count %d is more than %d requests\n", len(proxies), cfg.count, maxRequests)
		return exitUsage
	}

	var onResult func(*result)
	if cfg.output == "jsonl" {
		onResult = streamResult(stdout)
	}
	results := sample(cfg, proxies, newProgress(cfg, stderr, len(proxies)*cfg.count), onResult)
	rep := newReport(results)
	if err := writeReport(stdout, cfg.output, rep); err != nil {
		fmt.Fprintf(stderr, "proxysampler: %v\n", err)
		return exitUsage
	}

	if rep.SuccessRate < cfg.minSuccess {
		return exitBelowMin
	}
	return exitOK
}

const usage = `Usage:
  proxysampler [OPTIONS]

Test one proxy with --proxy, or a list of proxies with --file (one proxy URL
per line; blank lines and lines starting with # are skipped). Supported proxy
schemes are http, https, socks5, and socks5h.

Without --proxy or --file, ProxySampler tests proxy.packetstream.io with the
username and auth key in the PACKETSTREAM_USER and PACKETSTREAM_AUTH_KEY
environment variables, and the --protocol, --country, and --session options.

Options:
`

func parseFlags(args []string, stderr io.Writer) (config, error) {
	cfg := config{expectStatus: statusSet{}}
	var delayMS int

	fs := flag.NewFlagSet("proxysampler", flag.ContinueOnError)
	// Parse errors are printed once by run, without the usage text.
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	printUsage := func() {
		fmt.Fprint(stderr, usage)
		fs.VisitAll(func(f *flag.Flag) {
			name, text := flag.UnquoteUsage(f)
			fmt.Fprintf(stderr, "  %s\n      %s", strings.TrimSpace("--"+f.Name+" "+name), text)
			if f.DefValue != "" && f.DefValue != "false" && f.DefValue != "0" {
				fmt.Fprintf(stderr, " (default %s)", f.DefValue)
			}
			fmt.Fprintln(stderr)
		})
		fmt.Fprint(stderr, `
Exit status is 0 when the success rate is at least --min-success, 1 when it
is lower, and 2 for usage errors.
`)
	}
	fs.StringVar(&cfg.output, "output", "plaintext", "report format: plaintext, json, jsonl, or yaml")
	fs.BoolVar(&cfg.includeResponseBody, "include-response-body", false,
		"include response bodies (up to 1 MiB each) in JSON, JSONL, and YAML reports")
	fs.StringVar(&cfg.proxyFile, "file", "", "`path` to a file with one proxy URL per line")
	fs.StringVar(&cfg.singleProxy, "proxy", "", "proxy `URL` to test, such as https://user:pass@proxy.packetstream.io:31111")
	fs.StringVar(&cfg.endpoint, "endpoint", "", "`URL` to request through each proxy (default https://example.com, or "+exitCheckURL+" with --check-exit)")
	fs.IntVar(&cfg.maxThreads, "max-threads", 10, "number of requests to run at once")
	fs.IntVar(&delayMS, "delay", 50, "minimum time in `milliseconds` between the start of two requests")
	fs.DurationVar(&cfg.timeout, "timeout", 30*time.Second, "time limit for each request, including the proxy connection")
	fs.Var(&cfg.expectStatus, "expect-status", "HTTP status `codes` that count as success, comma-separated (default 200-299)")
	fs.Float64Var(&cfg.minSuccess, "min-success", 100, "minimum success rate, in `percent`, for exit status 0")
	fs.IntVar(&cfg.count, "count", 1, "number of requests to make through each proxy")
	fs.BoolVar(&cfg.checkExit, "check-exit", false,
		"look up each exit IP and country with "+exitCheckURL+"; requests that exit outside their _country- target fail")
	fs.StringVar(&cfg.protocol, "protocol", "https", "PacketStream proxy `protocol`: https, http, or socks5")
	fs.StringVar(&cfg.country, "country", "", "PacketStream exit `country`, as a two-letter code such as US")
	fs.StringVar(&cfg.session, "session", "", "PacketStream sticky session `name`, which keeps the same exit IP")
	fs.StringVar(&cfg.host, "host", "proxy.packetstream.io", "PacketStream proxy `host`")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printUsage()
		}
		return cfg, err
	}
	if fs.NArg() > 0 {
		// The argument is not quoted, because it may be a proxy URL with a password.
		return cfg, errors.New("unexpected argument; give the proxy with --proxy URL")
	}

	switch {
	case !slices.Contains([]string{"plaintext", "json", "jsonl", "yaml"}, cfg.output):
		return cfg, fmt.Errorf("invalid --output %q: use plaintext, json, jsonl, or yaml", cfg.output)
	case cfg.proxyFile != "" && cfg.singleProxy != "":
		return cfg, errors.New("use either --proxy or --file, not both")
	case cfg.count < 1 || cfg.count > maxRequests:
		return cfg, fmt.Errorf("--count must be between 1 and %d", maxRequests)
	case cfg.checkExit && cfg.endpoint != "" && cfg.endpoint != exitCheckURL:
		return cfg, errors.New("--check-exit uses its own endpoint, so it cannot be combined with --endpoint")
	case cfg.maxThreads < 1:
		return cfg, errors.New("--max-threads must be at least 1")
	case delayMS < 0:
		return cfg, errors.New("--delay must not be negative")
	case cfg.timeout <= 0:
		return cfg, errors.New("--timeout must be positive")
	case cfg.minSuccess < 0 || cfg.minSuccess > 100:
		return cfg, errors.New("--min-success must be between 0 and 100")
	}
	if err := checkPacketStreamFlags(cfg, fs); err != nil {
		return cfg, err
	}
	switch {
	case cfg.checkExit:
		cfg.endpoint = exitCheckURL
	case cfg.endpoint == "":
		cfg.endpoint = "https://example.com"
	}
	if err := checkEndpoint(cfg.endpoint); err != nil {
		return cfg, err
	}
	cfg.delay = time.Duration(delayMS) * time.Millisecond

	return cfg, nil
}

// statusSet holds the HTTP status codes that count as success. An empty set
// means any 2xx status.
type statusSet map[int]bool

func (s statusSet) String() string {
	codes := make([]string, 0, len(s))
	for code := range s {
		codes = append(codes, strconv.Itoa(code))
	}
	return strings.Join(codes, ",")
}

func (s statusSet) Set(value string) error {
	for field := range strings.SplitSeq(value, ",") {
		code, err := strconv.Atoi(strings.TrimSpace(field))
		if err != nil || code < 100 || code > 599 {
			return fmt.Errorf("invalid HTTP status code %q", field)
		}
		s[code] = true
	}
	return nil
}

func (s statusSet) ok(code int) bool {
	if len(s) == 0 {
		return code >= 200 && code <= 299
	}
	return s[code]
}
