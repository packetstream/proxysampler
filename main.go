package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/cheggaaa/pb"
)

type latency struct {
	TTFB         int64 `json:"ttfb" yaml:"ttfb"`
	Connect      int64 `json:"connect" yaml:"connect"`
	TLSHandshake int64 `json:"tls_handshake" yaml:"tls_handshake"`
}

type result struct {
	Proxy        string  `json:"proxy" yaml:"proxy"`
	Endpoint     string  `json:"endpoint" yaml:"endpoint"`
	StatusCode   int     `json:"status_code" yaml:"status_code"`
	ResponseBody string  `json:"response_body omitempty" yaml:"response_body,omitempty"`
	Latency      latency `json:"latency" yaml:"latency"`
	Err          error   `json:"error" yaml:"error"`
}

type report struct {
	Success     int       `json:"success" yaml:"success"`
	Fail        int       `json:"fail" yaml:"fail"`
	AverageTTFB int64     `json:"average_ttfb" yaml:"average_ttfb"`
	Results     []*result `json:"results" yaml:"results"`
}

var (
	results             []*result
	bar                 *pb.ProgressBar
	output              = "plaintext"
	proxyFile           = ""
	singleProxy         = ""
	testURL             = "https://example.com"
	delay               = 50
	maxThreads          = 10
	includeResponseBody = false
)

func init() {
	for k, v := range os.Args {
		// Handle arguments that don't require a parameter
		switch v {
		case "--include-response-body":
			includeResponseBody = true
		case "--help", "-h":
			showHelp()

			return
		}

		// Handle Arguments that require a parameter
		if len(os.Args) < k+2 {
			break
		}

		switch v {
		case "--output":
			setOutput(os.Args[k+1])
		case "--file":
			proxyFile = os.Args[k+1]
		case "--proxy":
			singleProxy = os.Args[k+1]
		case "--endpoint":
			testURL = os.Args[k+1]
		case "--max-threads":
			var err error
			maxThreads, err = strconv.Atoi(os.Args[k+1])
			if err != nil {
				maxThreads = 10

				break
			}
			if maxThreads < 1 {
				panic("invalid value for --max-threads")
			}
		case "--delay":
			var err error
			delay, err = strconv.Atoi(os.Args[k+1])
			if err != nil {
				delay = 50

				break
			}
			if delay < 0 {
				panic("invalid value for delay")
			}
		}
	}

	if proxyFile == "" && singleProxy == "" {
		panic("Use --file or --proxy to specify the proxy server(s) that you want to test")
	}
}

func main() {
	if singleProxy != "" {
		testProxies([]string{singleProxy})

		return
	}

	testProxiesFromFile(proxyFile)
}

// setOutput validates and sets output format selection.
func setOutput(outputType string) {
	switch outputType {
	case "json", "yaml", "plaintext":
		output = outputType
	default:
		panic("invalid value for --output. supported types are 'json', 'yaml', and 'plaintext'")
	}
}

// showHelp displays the cli help menu.
func showHelp() {
	fmt.Print(`
Usage:
	proxysampler [OPTIONS]

Application Options:
	--output {json|yaml|plaintext}    Default is plaintext.
	--include-response-body           Include response bodies in JSON/YAML output. Disabled by default.
	--file {/path/to/file.txt}        Relative/absolute path to a file containing a list of proxies.
	--proxy {proxy info}              Test a single proxy. Example: https://proxyuser:proxypass@packetstream.io:31111
	--endpoint {https://example.com}  The endpoint that you want to use for testing.
	--max-threads {10}                Number of concurrent threads to use for testing proxy. Default is 10.
	--delay {50}                      Delay in ms between each request. Default is 50.

Help Options
	--help, -h                        Show this screen.
`)
	os.Exit(0)
}
