package main

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testUser     = "user"
	testPassword = "s3cret_country-US_session-abc"
)

type testEnv struct {
	proxy   *testProxy
	https   *httptest.Server
	http    *httptest.Server
	rootCAs *x509.CertPool
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	var exitRequests atomic.Int64
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/exit/us":
			_, _ = w.Write([]byte(`{"ip":"203.0.113.1","country":"US","city":"Austin","org":"AS64500 Example"}`))
			return
		case "/exit/rotating":
			fmt.Fprintf(w, `{"ip":"203.0.113.%d","country":"PT"}`, exitRequests.Add(1))
			return
		case "/exit/none":
			_, _ = w.Write([]byte(`{"error":"rate limited"}`))
			return
		case "/status/500":
			w.WriteHeader(http.StatusInternalServerError)
		case "/slow":
			time.Sleep(2 * time.Second)
		}
		_, _ = w.Write([]byte("hello"))
	})
	env := &testEnv{https: httptest.NewTLSServer(handler), http: httptest.NewServer(handler)}
	t.Cleanup(env.https.Close)
	t.Cleanup(env.http.Close)

	env.rootCAs = x509.NewCertPool()
	env.rootCAs.AddCert(env.https.Certificate())
	env.proxy = startTestProxy(t, testUser, testPassword, env.https.TLS.Certificates[0])
	return env
}

// runCLI runs proxysampler with JSON output and returns the exit status and
// the decoded report.
func (env *testEnv) runCLI(t *testing.T, args ...string) (int, report, string) {
	t.Helper()
	return env.runCLIWith(t, nil, args...)
}

// runCLIWith is runCLI with a function that changes the parsed config.
func (env *testEnv) runCLIWith(t *testing.T, change func(*config), args ...string) (int, report, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cfg, err := parseFlags(append([]string{"--output", "json", "--delay", "0"}, args...), &stderr)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	cfg.rootCAs = env.rootCAs
	if change != nil {
		change(&cfg)
	}
	code := execute(cfg, &stdout, &stderr)

	var rep report
	if cfg.output != "json" {
		return code, rep, stdout.String()
	}
	if err := json.Unmarshal(stdout.Bytes(), &rep); err != nil {
		t.Fatalf("decoding report: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), "s3cret") {
		t.Errorf("output contains the proxy password:\n%s%s", stdout.String(), stderr.String())
	}
	return code, rep, stdout.String()
}

func TestProxySchemes(t *testing.T) {
	env := newTestEnv(t)
	for _, scheme := range []string{"http", "https", "socks5", "socks5h"} {
		t.Run(scheme, func(t *testing.T) {
			code, rep, _ := env.runCLI(t, "--proxy", env.proxy.url(scheme, testPassword), "--endpoint", env.https.URL)
			if code != exitOK || rep.Success != 1 || !rep.Results[0].OK || rep.Results[0].StatusCode != http.StatusOK {
				t.Fatalf("exit %d, report %+v, result %+v", code, rep, rep.Results[0])
			}
			if got := rep.Results[0].Proxy; !strings.Contains(got, ":xxxxx_country-US_session-abc@") {
				t.Errorf("proxy shown as %q, want password redacted and modifiers kept", got)
			}
		})
	}
}

func TestFailuresAreCounted(t *testing.T) {
	env := newTestEnv(t)
	tests := []struct {
		name      string
		proxy     string
		endpoint  string
		wantError string
	}{
		// Issue #3: a 407 from the proxy was counted as a success.
		{"wrong password, plain HTTP endpoint", env.proxy.url("http", "wrong"), env.http.URL, "407"},
		{"wrong password, HTTPS endpoint", env.proxy.url("http", "wrong"), env.https.URL, "Proxy Authentication Required"},
		{"wrong password, HTTPS proxy", env.proxy.url("https", "wrong"), env.https.URL, "Proxy Authentication Required"},
		{"wrong password, SOCKS5", env.proxy.url("socks5", "wrong"), env.https.URL, "username/password authentication failed"},
		{"endpoint error", env.proxy.url("http", testPassword), env.https.URL + "/status/500", "unexpected status 500"},
		{"proxy unreachable", "http://user:" + testPassword + "@127.0.0.1:1", env.https.URL, "connection refused"},
		{"timeout", env.proxy.url("socks5", testPassword), env.https.URL + "/slow", "timed out"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, rep, _ := env.runCLI(t, "--proxy", tt.proxy, "--endpoint", tt.endpoint, "--timeout", "1s")
			if code != exitBelowMin || rep.Fail != 1 || rep.Success != 0 {
				t.Fatalf("exit %d, success %d, fail %d; want exit 1 and one failure", code, rep.Success, rep.Fail)
			}
			if got := rep.Results[0].Error; !strings.Contains(got, tt.wantError) {
				t.Errorf("error %q, want it to contain %q", got, tt.wantError)
			}
		})
	}
}

func TestExpectStatus(t *testing.T) {
	env := newTestEnv(t)
	code, rep, _ := env.runCLI(t, "--proxy", env.proxy.url("http", testPassword),
		"--endpoint", env.https.URL+"/status/500", "--expect-status", "500,503")
	if code != exitOK || rep.Success != 1 {
		t.Fatalf("exit %d, success %d; want 500 accepted", code, rep.Success)
	}
}

func TestProxyFile(t *testing.T) {
	env := newTestEnv(t)
	file := filepath.Join(t.TempDir(), "proxies.txt")
	content := strings.Join([]string{
		"# comment",
		env.proxy.url("http", testPassword),
		"",
		"   ",
		env.proxy.url("socks5", "wrong"),
		env.proxy.url("https", testPassword),
		env.proxy.url("socks5", testPassword),
	}, "\n")
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	code, rep, _ := env.runCLI(t, "--file", file, "--endpoint", env.https.URL)
	if code != exitBelowMin || rep.Success != 3 || rep.Fail != 1 || rep.SuccessRate != 75 {
		t.Fatalf("exit %d, success %d, fail %d, rate %v; want 1, 3, 1, 75", code, rep.Success, rep.Fail, rep.SuccessRate)
	}
	// Results keep the order of the file.
	if !strings.HasPrefix(rep.Results[1].Proxy, "socks5://") || rep.Results[1].OK {
		t.Errorf("second result %+v, want the failed SOCKS5 proxy", rep.Results[1])
	}

	code, _, _ = env.runCLI(t, "--file", file, "--endpoint", env.https.URL, "--min-success", "75")
	if code != exitOK {
		t.Errorf("exit %d with --min-success 75, want 0", code)
	}
}

func TestResponseBody(t *testing.T) {
	env := newTestEnv(t)
	proxyURL := env.proxy.url("http", testPassword)

	_, _, out := env.runCLI(t, "--proxy", proxyURL, "--endpoint", env.https.URL)
	if strings.Contains(out, "response_body") {
		t.Errorf("report includes response_body without --include-response-body:\n%s", out)
	}

	_, rep, _ := env.runCLI(t, "--proxy", proxyURL, "--endpoint", env.https.URL, "--include-response-body")
	if rep.Results[0].ResponseBody != "hello" {
		t.Errorf("response body %q, want hello", rep.Results[0].ResponseBody)
	}
}

func TestUsageErrors(t *testing.T) {
	t.Setenv("PACKETSTREAM_USER", "")
	t.Setenv("PACKETSTREAM_AUTH_KEY", "")
	tests := [][]string{
		{},
		{"--proxy", "http://user:pass@host:1", "--file", "x"},
		{"--proxy", "http://user:pass@host:1", "--output", "xml"},
		{"--proxy", "http://user:pass@host:1", "--max-threads", "0"},
		{"--proxy", "http://user:pass@host:1", "--max-threads", "many"},
		{"--proxy", "http://user:pass@host:1", "--delay", "-1"},
		{"--proxy", "http://user:pass@host:1", "--min-success", "101"},
		{"--proxy", "http://user:pass@host:1", "--expect-status", "abc"},
		{"--proxy", "http://user:pass@host:1", "--endpoint", "example.com"},
		{"--proxy", "ftp://user:s3cret@host:1"},
		{"--proxy", "user:s3cret@host:1"},
		{"--proxy", "http://user:s3cret@host"},
		{"--file", filepath.Join(os.TempDir(), "proxysampler-missing.txt")},
		{"--proxy", "http://user:pass@host:1", "extra"},
		{"https://user:s3cret@host:1"},
		{"--bogus"},
		{"--proxy", "http://user:pass@host:1", "--count", "0"},
		{"--proxy", "http://user:pass@host:1", "--count", "9223372036854775807"},
		{"--proxy", "http://user:pass@host:1", "--country", "US"},
		{"--file", "proxies.txt", "--session", "abc"},
		{"--protocol", "ftp"},
		{"--country", "USA"},
		{"--session", "a_b"},
		{"--check-exit", "--endpoint", "https://example.com"},
	}
	for _, args := range tests {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(args, &stdout, &stderr); code != exitUsage {
				t.Errorf("exit %d, want %d", code, exitUsage)
			}
			if stdout.Len() > 0 || strings.Count(stderr.String(), "proxysampler:") != 1 {
				t.Errorf("stdout %q, stderr %q; want only an error on stderr", stdout.String(), stderr.String())
			}
			if strings.Contains(stderr.String(), "s3cret") {
				t.Errorf("error shows the password: %s", stderr.String())
			}
		})
	}

	var stdout, stderr bytes.Buffer
	if code := run([]string{"--help"}, &stdout, &stderr); code != exitOK || !strings.Contains(stderr.String(), "--min-success") {
		t.Errorf("--help: exit %d, output %q", code, stderr.String())
	}
}

func TestRedactProxy(t *testing.T) {
	tests := map[string]string{
		"https://u:key_country-US_session-a1@proxy.packetstream.io:31111": "https://u:xxxxx_country-US_session-a1@proxy.packetstream.io:31111",
		"http://u:key_session-a1@host:1":                                  "http://u:xxxxx_session-a1@host:1",
		"http://u:my_secret_pw@host:1":                                    "http://u:xxxxx@host:1",
		"socks5://u:p@host:1":                                             "socks5://u:xxxxx@host:1",
		"http://u@host:1":                                                 "http://u@host:1",
		"http://host:1":                                                   "http://host:1",
	}
	for raw, want := range tests {
		p, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := redactProxy(p); got != want {
			t.Errorf("redactProxy(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestRedactError(t *testing.T) {
	p, err := url.Parse("http://user:p@host:1")
	if err != nil {
		t.Fatal(err)
	}
	msg := "proxyconnect tcp: dial tcp 10.0.0.1:1: connect: connection refused"
	if got := redactError(errors.New(msg), p); got != msg {
		t.Errorf("redactError changed an error without the password: %q", got)
	}

	p, err = url.Parse("http://user:a%2Fb@host:1")
	if err != nil {
		t.Fatal(err)
	}
	for _, msg := range []string{`Get "http://user:a/b@host:1"`, `Get "http://user:a%2Fb@host:1"`} {
		if got := redactError(errors.New(msg), p); strings.Contains(got, "a/b") || strings.Contains(got, "a%2Fb") {
			t.Errorf("redactError(%q) = %q, want the password removed", msg, got)
		}
	}
}

func TestCount(t *testing.T) {
	env := newTestEnv(t)
	code, rep, _ := env.runCLI(t, "--proxy", env.proxy.url("socks5", testPassword), "--endpoint", env.https.URL, "--count", "5")
	if code != exitOK || len(rep.Results) != 5 || rep.Success != 5 {
		t.Fatalf("exit %d, %d results, %d successes; want 0, 5, 5", code, len(rep.Results), rep.Success)
	}
	if rep.StatusCodes[200] != 5 || rep.TTFB == nil || rep.TTFB.Max < rep.TTFB.P50 {
		t.Errorf("status codes %v, TTFB %+v", rep.StatusCodes, rep.TTFB)
	}
}

func TestCheckExit(t *testing.T) {
	env := newTestEnv(t)
	tests := []struct {
		name        string
		path        string
		count       string
		wantOK      int
		wantError   string
		wantChanged int
	}{
		{"requested country", "/exit/us", "3", 3, "", 0},
		// testPassword requests US.
		{"wrong country", "/exit/rotating", "1", 0, "exit country PT, requested US", 0},
		{"no exit IP", "/exit/none", "1", 0, "exit check response has no IP address", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, rep, _ := env.runCLIWith(t, func(cfg *config) { cfg.endpoint = env.https.URL + tt.path },
				"--proxy", env.proxy.url("http", testPassword), "--check-exit", "--count", tt.count)
			if rep.Success != tt.wantOK {
				t.Fatalf("%d successes, want %d: %+v", rep.Success, tt.wantOK, rep.Results[0])
			}
			if tt.wantError != "" && rep.Errors[tt.wantError] == 0 {
				t.Errorf("errors %v, want %q", rep.Errors, tt.wantError)
			}
		})
	}

	_, rep, _ := env.runCLIWith(t, func(cfg *config) { cfg.endpoint = env.https.URL + "/exit/us" },
		"--proxy", env.proxy.url("http", testPassword), "--check-exit", "--count", "3")
	want := exitSummary{UniqueIPs: 1, Countries: map[string]int{"US": 3}, StickySessions: 1}
	if rep.Exits == nil || rep.Exits.UniqueIPs != want.UniqueIPs || rep.Exits.Countries["US"] != 3 ||
		rep.Exits.StickySessions != 1 || rep.Exits.SessionsChangedIP != 0 {
		t.Errorf("exits %+v, want %+v", rep.Exits, want)
	}
	if e := rep.Results[0].Exit; e == nil || e.City != "Austin" {
		t.Errorf("exit %+v, want the exit check response", e)
	}
}

func TestStickySessionChangedIP(t *testing.T) {
	env := newTestEnv(t)
	// A session with no country modifier, so the PT exits are accepted.
	password := "s3cret_session-abc"
	env.proxy.password = password
	_, rep, _ := env.runCLIWith(t, func(cfg *config) { cfg.endpoint = env.https.URL + "/exit/rotating" },
		"--proxy", env.proxy.url("http", password), "--check-exit", "--count", "3")
	if rep.Success != 3 || rep.Exits == nil || rep.Exits.UniqueIPs != 3 || rep.Exits.SessionsChangedIP != 1 {
		t.Fatalf("success %d, exits %+v; want 3 successes and one session that changed IP", rep.Success, rep.Exits)
	}
}

func TestJSONLines(t *testing.T) {
	env := newTestEnv(t)
	_, _, out := env.runCLI(t, "--proxy", env.proxy.url("http", testPassword), "--endpoint", env.https.URL,
		"--count", "3", "--output", "jsonl")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 4 {
		t.Fatalf("%d lines, want 3 results and a summary:\n%s", len(lines), out)
	}
	for i, line := range lines {
		var record struct {
			Type    string    `json:"type"`
			OK      bool      `json:"ok"`
			Success int       `json:"success"`
			Results []*result `json:"results"`
		}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("line %d: %v", i+1, err)
		}
		switch {
		case i < 3 && (record.Type != "result" || !record.OK):
			t.Errorf("line %d: %s, want a successful result", i+1, line)
		case i == 3 && (record.Type != "summary" || record.Success != 3 || record.Results != nil):
			t.Errorf("summary %s, want 3 successes and no results", line)
		}
	}
}

func TestPacketStreamProxy(t *testing.T) {
	t.Setenv("PACKETSTREAM_USER", "alice")
	t.Setenv("PACKETSTREAM_AUTH_KEY", "key123")
	tests := []struct {
		args []string
		want string
	}{
		{nil, "https://alice:key123@proxy.packetstream.io:31111"},
		{[]string{"--protocol", "http", "--country", "us"}, "http://alice:key123_country-US@proxy.packetstream.io:31112"},
		{[]string{"--protocol", "socks5", "--country", "GB", "--session", "s-1"}, "socks5://alice:key123_country-GB_session-s-1@proxy.packetstream.io:31113"},
		{[]string{"--host", "127.0.0.1", "--session", "abc"}, "https://alice:key123_session-abc@127.0.0.1:31111"},
	}
	for _, tt := range tests {
		cfg, err := parseFlags(tt.args, io.Discard)
		if err != nil {
			t.Fatalf("%v: %v", tt.args, err)
		}
		proxies, err := loadProxies(cfg)
		if err != nil {
			t.Fatalf("%v: %v", tt.args, err)
		}
		if got := proxies[0].String(); got != tt.want {
			t.Errorf("%v: proxy %q, want %q", tt.args, got, tt.want)
		}
	}
}
