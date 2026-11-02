package main

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

// maxBodyBytes limits how much of each response is read.
const maxBodyBytes = 1 << 20

type latency struct {
	Connect      int64 `json:"connect" yaml:"connect"`
	TLSHandshake int64 `json:"tls_handshake" yaml:"tls_handshake"`
	TTFB         int64 `json:"ttfb" yaml:"ttfb"`
	Total        int64 `json:"total" yaml:"total"`
}

type result struct {
	Proxy        string  `json:"proxy" yaml:"proxy"`
	Endpoint     string  `json:"endpoint" yaml:"endpoint"`
	OK           bool    `json:"ok" yaml:"ok"`
	StatusCode   int     `json:"status_code,omitempty" yaml:"status_code,omitempty"`
	Error        string  `json:"error,omitempty" yaml:"error,omitempty"`
	ResponseBody string  `json:"response_body,omitempty" yaml:"response_body,omitempty"`
	Latency      latency `json:"latency" yaml:"latency"`
}

// sample requests the endpoint once through each proxy and returns the
// results in the same order as the proxies.
func sample(cfg config, proxies []*url.URL, prog *progress) []*result {
	results := make([]*result, len(proxies))
	jobs := make(chan int)

	var wg sync.WaitGroup
	for range min(cfg.maxThreads, len(proxies)) {
		wg.Go(func() {
			for i := range jobs {
				results[i] = request(cfg, proxies[i])
				prog.done()
			}
		})
	}

	for i := range proxies {
		if i > 0 && cfg.delay > 0 {
			time.Sleep(cfg.delay)
		}
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	prog.finish()

	return results
}

// request makes one request through a proxy, on a new connection.
func request(cfg config, p *url.URL) *result {
	res := &result{Proxy: redactProxy(p), Endpoint: cfg.endpoint}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	transport, err := newTransport(cfg, p)
	if err != nil {
		res.Error = redactError(err, p)
		return res
	}
	defer transport.CloseIdleConnections()

	// Trace callbacks can run on a dial goroutine after a timeout, so they
	// record into tm, which is copied into res once when request returns.
	var start time.Time
	var tm timings
	defer tm.copyTo(&res.Latency)
	var connectStart, tlsStart time.Time
	trace := &httptrace.ClientTrace{
		ConnectStart: func(_, _ string) { tm.record(func(*latency) { connectStart = time.Now() }) },
		ConnectDone: func(_, _ string, _ error) {
			tm.record(func(l *latency) { l.Connect = time.Since(connectStart).Milliseconds() })
		},
		// An HTTPS proxy adds a second handshake, so handshakes add up.
		TLSHandshakeStart: func() { tm.record(func(*latency) { tlsStart = time.Now() }) },
		TLSHandshakeDone: func(tls.ConnectionState, error) {
			tm.record(func(l *latency) { l.TLSHandshake += time.Since(tlsStart).Milliseconds() })
		},
		GotFirstResponseByte: func() {
			tm.record(func(l *latency) { l.TTFB = time.Since(start).Milliseconds() })
		},
	}

	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodGet, cfg.endpoint, http.NoBody)
	if err != nil {
		res.Error = err.Error()
		return res
	}

	start = time.Now()
	resp, err := transport.RoundTrip(req)
	if err != nil {
		res.Error = describeError(err, p)
		return res
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	res.Latency.Total = time.Since(start).Milliseconds()
	res.StatusCode = resp.StatusCode
	if cfg.includeResponseBody {
		res.ResponseBody = string(body)
	}

	switch {
	case err != nil:
		res.Error = "reading response: " + describeError(err, p)
	case !cfg.expectStatus.ok(resp.StatusCode):
		res.Error = "unexpected status " + resp.Status
	default:
		res.OK = true
	}
	return res
}

// timings collects latencies from trace callbacks.
type timings struct {
	mu      sync.Mutex
	latency latency
	done    bool
}

func (t *timings) record(f func(*latency)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.done {
		f(&t.latency)
	}
}

// copyTo copies the trace latencies into l and ignores later callbacks.
func (t *timings) copyTo(l *latency) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.done = true
	l.Connect, l.TLSHandshake, l.TTFB = t.latency.Connect, t.latency.TLSHandshake, t.latency.TTFB
}

func newTransport(cfg config, p *url.URL) (*http.Transport, error) {
	transport := &http.Transport{
		DisableKeepAlives:     true,
		TLSClientConfig:       &tls.Config{RootCAs: cfg.rootCAs},
		TLSHandshakeTimeout:   cfg.timeout,
		ResponseHeaderTimeout: cfg.timeout,
	}

	switch p.Scheme {
	case "http", "https":
		transport.Proxy = http.ProxyURL(p)
	case "socks5", "socks5h":
		// Both schemes resolve the destination through the proxy.
		dialer, err := proxy.FromURL(p, &net.Dialer{Timeout: cfg.timeout})
		if err != nil {
			return nil, err
		}
		contextDialer, ok := dialer.(proxy.ContextDialer)
		if !ok {
			return nil, errors.New("SOCKS5 dialer does not support timeouts")
		}
		transport.DialContext = contextDialer.DialContext
	}
	return transport, nil
}

// describeError turns a request error into a short, readable message without
// the proxy password.
func describeError(err error, p *url.URL) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		err = urlErr.Err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timed out"
	}
	return redactError(err, p)
}
