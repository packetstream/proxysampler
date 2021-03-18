package main

import (
	"bufio"
	"crypto/tls"
	"io/ioutil"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/cheggaaa/pb"
)

// getHTTP fetches a resource using a supplied proxy and returns a result item for the report
func getHTTP(fetchURL string, proxy string) (res *result, err error) {
	res = &result{}
	res.Proxy = proxy
	res.Endpoint = fetchURL
	req, _ := http.NewRequest("GET", fetchURL, nil)

	var start, connect, tlsHandshake time.Time

	// Measure response times
	trace := &httptrace.ClientTrace{
		TLSHandshakeStart: func() { tlsHandshake = time.Now() },
		TLSHandshakeDone: func(cs tls.ConnectionState, err error) {
			res.Latency.TLSHandshake = time.Since(tlsHandshake).Nanoseconds() / 1000000
		},

		ConnectStart: func(network, addr string) { connect = time.Now() },
		ConnectDone: func(network, addr string, err error) {
			res.Latency.Connect = time.Since(connect).Nanoseconds() / 1000000
		},

		GotFirstResponseByte: func() {
			res.Latency.TTFB = time.Since(start).Nanoseconds() / 1000000
		},
	}

	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	var r *http.Response

	start = time.Now()

	// Set proxy
	p, err := url.Parse(proxy)
	if err != nil {
		panic(err)
	}
	tr := http.Transport{
		Proxy:                 http.ProxyURL(p),
		TLSClientConfig:       &tls.Config{},
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		ExpectContinueTimeout: 5 * time.Second,
		DisableKeepAlives:     true,
		MaxConnsPerHost:       0,
	}

	if r, err = tr.RoundTrip(req); err != nil {
		res.StatusCode = -1
		return
	}

	// Read response body
	if includeResponseBody && r.Body != nil {
		b, err := ioutil.ReadAll(r.Body)
		if err != nil {
			res.ResponseBody = "ERROR PARSING RESPONSE"
		} else {
			res.ResponseBody = string(b)
		}
	}
	r.Body.Close()

	res.StatusCode = r.StatusCode
	return
}

// testProxies takes a slice of strings with proxy information, calls getHTTP to test them, and runs the report when finished
func testProxies(proxies []string) {
	proxiesCh := make(chan string, maxThreads)
	resultsCh := make(chan *result)
	done := make(chan struct{})

	var wg sync.WaitGroup

	for w := 1; w <= maxThreads; w++ {
		// Run getHTTP calls a concurrently
		wg.Add(1)
		go func(proxies chan string, results chan *result) {
			defer wg.Done()
			for proxy := range proxies {
				time.Sleep(time.Duration(delay) * time.Millisecond)
				res, err := getHTTP(testURL, proxy)
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

	<- done
}

// testProxiesFromFile reads a file for proxy information and passes them to testProxies
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
		// TODO: validate format?
		lines = append(lines, scanner.Text())
		remainingThreads++
	}

	// Start the progress bar
	if output == "plaintext" {
		bar = pb.StartNew(len(lines))
	}

	if err := scanner.Err(); err != nil {
		panic(err)
	}

	// Test proxies
	testProxies(lines)
}
