package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/cheggaaa/pb"
)

type latency struct {
	TTFB         int64 `json:"ttfb",yaml:"ttfb"`
	Connect      int64 `json:"connect",yaml:"connect"`
	TLSHandshake int64 `json:"tls_handshake",yaml:"tls_handshake"`
}

type result struct {
	Proxy        string  `json:"proxy",yaml:"proxy"`
	Endpoint     string  `json:"endpoint",yaml:"endpoint"`
	StatusCode   int     `json:"status_code",yaml:"status_code"`
	ResponseBody string  `json:"response_body,omitempty",yaml:"response_body,omitempty"`
	Latency      latency `json:"latency",yaml:"latency"`
	Err          error   `json:"error",yaml:"error"`
}

type report struct {
	Success     int       `json:"success",yaml:"success"`
	Fail        int       `json:"fail",yaml:"fail"`
	AverageTTFB int64     `json:"average_ttfb",yaml:"average_ttfb"`
	Results     []*result `json:"results",yaml:"results"`
}

var results []*result
var activeThreads = 0
var remainingThreads = 0
var bar *pb.ProgressBar
var output = "plaintext"
var proxyFile = ""
var singleProxy = ""
var testURL = "https://example.com"
var delay = 50
var maxThreads = 10
var includeResponseBody = false

// init reads command line arguments and sets config variables
func init() {
	for k, v := range os.Args {
		// Handle arguments that don't require a parameter
		switch v {
		case "--include-response-body":
			includeResponseBody = true
			break
		case "--help", "-h":
			showHelp()
			return
		default:
			break
		}

		// Handle Arguments that require a parameter
		if len(os.Args) < k+2 {
			break
		}

		switch v {
		case "--output":
			setOutput(os.Args[k+1])
			break
		case "--file":
			proxyFile = os.Args[k+1]
			break
		case "--proxy":
			singleProxy = os.Args[k+1]
			break
		case "--endpoint":
			testURL = os.Args[k+1]
			break
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
			break
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
			break
		default:
			break
		}
	}

	if proxyFile == "" && singleProxy == "" {
		panic("Use --file or --proxy to specify the proxy server(s) that you want to test")
	}
}

// main
func main() {
	activeThreads = maxThreads
	if singleProxy != "" {
		testProxies([]string{singleProxy})
		return
	}

	testProxiesFromFile(proxyFile)
}

// setOutput validates output format selection
func setOutput(outputType string) {
	switch outputType {
	case "json", "yaml", "plaintext":
		output = outputType
		break
	default:
		panic("invalid value for --output. supported types are 'json', 'yaml', and 'plaintext'")
	}
}

// showHelp displays the cli help menu
func showHelp() {
	fmt.Println(`
Usage:
	proxysampler [OPTIONS]

Application Options:
	--output {json|yaml|plaintext}    Default is plaintext.
	--include-response-body           Include response bodies in JSON/YAML output. Disabled by default.
	--file {/path/to/file.txt}        Relative/absolute path to a file containing a list of proxies.
	--proxy {proxy info}              Test a single proxy tunnel. Example: https://proxyuser:proxypass@packetstream.io:31111
	--endpoint {https://example.com}  The endpoint that you want to use for testing.
	--max-threads {10}                Number of concurrent threads to use for testing proxy. Default is 10.
	--delay {50}                      Delay in ms between each request. Default is 50.

Help Options
	--help, -h                        Show this screen.
`)
	os.Exit(0)
}
