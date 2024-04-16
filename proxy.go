package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/cheggaaa/pb"
	"golang.org/x/net/proxy"
)

const timeout = 5 * time.Second

// proxiedRequest makes a request to an endpoint using a proxy.
func proxiedRequest(proxyURL string, endpoint string) (*result, error) {
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	var start, connect, tlsHandshake time.Time

	res := &result{
		Proxy:    proxyURL,
		Endpoint: endpoint,
	}

	// Measure response times
	trace := &httptrace.ClientTrace{
		TLSHandshakeStart: func() { tlsHandshake = time.Now() },
		TLSHandshakeDone: func(cs tls.ConnectionState, err error) {
			res.Latency.TLSHandshake = time.Since(tlsHandshake).Nanoseconds() / 1000000 //nolint:gomnd
		},

		ConnectStart: func(network, addr string) { connect = time.Now() },
		ConnectDone: func(network, addr string, err error) {
			res.Latency.Connect = time.Since(connect).Nanoseconds() / 1000000 //nolint:gomnd
		},

		GotFirstResponseByte: func() {
			res.Latency.TTFB = time.Since(start).Nanoseconds() / 1000000 //nolint:gomnd
		},
	}

	// Create a new request with the trace context
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	start = time.Now()
	p, err := url.Parse(proxyURL)
	if err != nil {
		return nil, err
	}

	tr := http.Transport{
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: timeout,
		DisableKeepAlives:     true,
		MaxConnsPerHost:       0,
	}

	// Create a client based on the proxy scheme
	switch p.Scheme {
	case "http", "https":
		// HTTP/HTTPS proxy
		tr.Proxy = http.ProxyURL(p)
	case "socks5", "socks5h":
		// SOCKS5 proxy
		dialer, err := proxy.FromURL(p, proxy.Direct)
		if err != nil {
			return nil, err
		}

		tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		}
	default:
		return nil, errors.New("unsupported proxy scheme: " + p.Scheme)
	}

	r, err := tr.RoundTrip(req)
	if err != nil {
		res.StatusCode = -1

		return res, err
	}
	defer r.Body.Close()

	res.StatusCode = r.StatusCode

	// Read response body
	if includeResponseBody && r.Body != nil {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			res.ResponseBody = "ERROR PARSING RESPONSE"
		} else {
			res.ResponseBody = string(b)
		}
	}

	return res, nil
}

// testProxies tests a list of proxies.
func testProxies(proxies []string) {
	// Start the progress bar
	if output == "plaintext" {
		bar = pb.StartNew(len(proxies))
	}

	proxiesCh := make(chan string, maxThreads)
	resultsCh := make(chan *result)
	done := make(chan struct{})

	var wg sync.WaitGroup

	for w := 1; w <= maxThreads; w++ {
		// Run concurrent workers
		wg.Add(1)
		go func(proxies chan string, results chan *result) {
			defer wg.Done()
			for proxy := range proxies {
				time.Sleep(time.Duration(delay) * time.Millisecond)
				res, err := proxiedRequest(proxy, testURL)
				if res == nil {
					res = &result{}
				}

				if err != nil {
					res.Err = err
				}

				results <- res
			}
		}(proxiesCh, resultsCh)
	}

	go func(resultsCh chan *result) {
		for res := range resultsCh {
			results = append(results, res)

			if output == "plaintext" {
				bar.Increment()
			}
		}

		// Stop the progress bar
		if output == "plaintext" {
			bar.Finish()
		}

		// Display the report
		displayReport(results)
		done <- struct{}{}
	}(resultsCh)

	for _, proxy := range proxies {
		proxiesCh <- proxy
	}

	close(proxiesCh)

	wg.Wait()

	close(resultsCh)

	<-done
}

// testProxiesFromFile tests proxies from a file.
func testProxiesFromFile(fileName string) {
	// Read proxy file
	file, err := os.Open(fileName)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		// Validate format
		_, err := url.Parse(scanner.Text())
		if err != nil {
			panic("Invalid proxy format: " + scanner.Text())
		}

		lines = append(lines, scanner.Text())
	}

	if err := scanner.Err(); err != nil {
		panic(err)
	}

	// Test proxies
	testProxies(lines)
}
