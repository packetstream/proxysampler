# ProxySampler

ProxySampler tests proxies and reports their success rate and response times.
Test one proxy or a list of them, and use the exit status in scripts and
monitoring: it is non-zero when too many requests fail.

It works with any HTTP, HTTPS, or SOCKS5 proxy, and was written for
[PacketStream](https://packetstream.io/) residential proxies.

## Installation

Install [Go](https://go.dev/dl/) 1.26 or newer, then run:

```bash
go install github.com/packetstream/proxysampler@latest
```

The binary is installed to `$(go env GOPATH)/bin`.

## Usage

### Test PacketStream

Set your PacketStream username and auth key, which you can find in the
[dashboard](https://app.packetstream.io/), then run ProxySampler without
`--proxy` or `--file`:

```bash
export PACKETSTREAM_USER=your_username
export PACKETSTREAM_AUTH_KEY=your_auth_key
proxysampler --protocol http --country US --session test1 --count 5 --check-exit
```

```
Success rate:      5/5 (100.0%)
TTFB:              avg 759ms  p50 768ms  p90 834ms  p99 834ms  max 834ms
Total time:        avg 759ms  p50 768ms  p90 834ms  p99 834ms  max 834ms
Status codes:      200×5
Exit IPs:          1 unique
Exit countries:    US×5
Sticky sessions:   1/1 kept one exit IP
```

Reading the key from the environment keeps it out of your shell history and
the process list. Choose the proxy with `--protocol` (`https`, `http`, or
`socks5`), the exit country with `--country`, and a sticky session with
`--session`. Without a session, each request usually gets a new exit IP:

```bash
proxysampler --protocol socks5 --country GB --count 10 --check-exit
```

`--check-exit` requests [ipinfo.io](https://ipinfo.io/json) to find each exit
IP and its country. A request that exits outside its requested country fails,
and the report shows whether each sticky session kept one exit IP.

### Test any proxy

Test one proxy:

```bash
proxysampler --proxy "https://USER:AUTH_KEY_country-US@proxy.packetstream.io:31111"
```

Test every proxy in a file, with a JSON report:

```bash
proxysampler --file proxies.txt --output json --endpoint https://ipinfo.io/json
```

The file has one proxy URL per line. Blank lines and lines starting with `#`
are skipped. See [proxies.txt](proxies.txt) for an example.

`--count` repeats the test through each proxy. Each request uses a new
connection. A request succeeds when the endpoint
answers with a 2xx status, or with one of the codes given to
`--expect-status`. Anything else counts as a failure, including a proxy that
rejects its credentials (407) or cannot reach the endpoint (502).

### PacketStream proxy URLs

| Protocol | Proxy URL |
| --- | --- |
| HTTPS | `https://USER:AUTH_KEY@proxy.packetstream.io:31111` |
| HTTP | `http://USER:AUTH_KEY@proxy.packetstream.io:31112` |
| SOCKS5 | `socks5://USER:AUTH_KEY@proxy.packetstream.io:31113` |

Add `_country-CC` to the auth key to choose the exit country, and
`_session-NAME` to keep the same exit IP across requests, for example
`AUTH_KEY_country-US_session-abc123`. `--check-exit` reads the requested
country from the proxy URL, so it works with `--proxy` and `--file` too.

### Options

```
  --check-exit
      look up each exit IP and country with https://ipinfo.io/json; requests that exit outside their _country- target fail
  --count int
      number of requests to make through each proxy (default 1)
  --country country
      PacketStream exit country, as a two-letter code such as US
  --delay milliseconds
      minimum time in milliseconds between the start of two requests (default 50)
  --endpoint URL
      URL to request through each proxy (default https://example.com, or https://ipinfo.io/json with --check-exit)
  --expect-status codes
      HTTP status codes that count as success, comma-separated (default 200-299)
  --file path
      path to a file with one proxy URL per line
  --host host
      PacketStream proxy host (default proxy.packetstream.io)
  --include-response-body
      include response bodies (up to 1 MiB each) in JSON, JSONL, and YAML reports
  --max-threads int
      number of requests to run at once (default 10)
  --min-success percent
      minimum success rate, in percent, for exit status 0 (default 100)
  --output string
      report format: plaintext, json, jsonl, or yaml (default plaintext)
  --protocol protocol
      PacketStream proxy protocol: https, http, or socks5 (default https)
  --proxy URL
      proxy URL to test, such as https://user:pass@proxy.packetstream.io:31111
  --session name
      PacketStream sticky session name, which keeps the same exit IP
  --timeout duration
      time limit for each request, including the proxy connection (default 30s)
```

Exit status is 0 when the success rate is at least `--min-success`, 1 when it
is lower, and 2 for usage errors.

### Reports

`--output json` and `--output yaml` print one report at the end.
`--output jsonl` prints each result as a JSON line as soon as it finishes,
with `"type": "result"`, then a line with `"type": "summary"` that has
everything but `results`.

```json
{
  "success": 1,
  "fail": 1,
  "success_rate": 50,
  "average_ttfb": 206,
  "ttfb": {"avg": 206, "p50": 206, "p90": 206, "p99": 206, "max": 206},
  "total": {"avg": 206, "p50": 206, "p90": 206, "p99": 206, "max": 206},
  "status_codes": {"200": 1},
  "errors": {"Proxy Authentication Required": 1},
  "results": [
    {
      "proxy": "https://USER:xxxxx_country-US@proxy.packetstream.io:31111",
      "endpoint": "https://example.com",
      "ok": true,
      "status_code": 200,
      "latency": {"connect": 31, "tls_handshake": 76, "ttfb": 206, "total": 206}
    },
    {
      "proxy": "https://USER:xxxxx_country-CA@proxy.packetstream.io:31111",
      "endpoint": "https://example.com",
      "ok": false,
      "error": "Proxy Authentication Required",
      "latency": {"connect": 30, "tls_handshake": 40, "ttfb": 0, "total": 0}
    }
  ]
}
```

Latencies are in milliseconds, and the summaries cover successful requests. `connect` is the TCP connection to the proxy,
`tls_handshake` adds up the TLS handshakes with an HTTPS proxy and with the
endpoint, `ttfb` runs from the start of the request to the first response
byte, and `total` includes reading up to 1 MiB of the response. With
`--check-exit`, each result has an `exit` object (`ip`, `country`, `city`,
`org`), and the report has an `exits` summary: `unique_ips`, `countries`,
`sticky_sessions` (session proxies with more than one successful request),
and `sessions_changed_ip`. Reports show
proxy passwords as `xxxxx`, keeping any `_country-` and `_session-` options.

### Upgrading from earlier versions

Version 1 fixed several reporting bugs, so its reports differ:

- A request succeeds only on an expected status. Earlier versions counted
  any HTTP answer, including 407 and 502, as a success.
- Errors are strings instead of `{}`, and each result has an `ok` field.
- `response_body` is spelled correctly and only appears with
  `--include-response-body`.
- Proxy passwords are redacted.
- Blank lines and comments in proxy files are skipped.
- `--delay` spaces out all requests instead of each thread's requests.
- The exit status reports failures, and invalid options print an error
  instead of a panic.

## Contributing

Bug reports and pull requests are welcome. See [CONTRIBUTING.md](CONTRIBUTING.md),
and report security issues as described in [SECURITY.md](SECURITY.md).

## License

ProxySampler is licensed under the [GNU General Public License v3.0](LICENSE).
