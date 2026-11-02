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

Test one proxy:

```bash
proxysampler --proxy "https://USER:AUTH_KEY_country-US@proxy.packetstream.io:31111"
```

```
Success rate:      1/1 (100.0%)
Average TTFB:      221ms
```

Test every proxy in a file, with a JSON report:

```bash
proxysampler --file proxies.txt --output json --endpoint https://ipinfo.io/json
```

The file has one proxy URL per line. Blank lines and lines starting with `#`
are skipped. See [proxies.txt](proxies.txt) for an example.

Each request uses a new connection. A request succeeds when the endpoint
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
`AUTH_KEY_country-US_session-abc123`. Find your username and auth key in the
[dashboard](https://app.packetstream.io/).

### Options

```
  --delay milliseconds
      minimum time in milliseconds between the start of two requests (default 50)
  --endpoint URL
      URL to request through each proxy (default https://example.com)
  --expect-status codes
      HTTP status codes that count as success, comma-separated (default 200-299)
  --file path
      path to a file with one proxy URL per line
  --include-response-body
      include response bodies (up to 1 MiB each) in JSON and YAML reports
  --max-threads int
      number of requests to run at once (default 10)
  --min-success percent
      minimum success rate, in percent, for exit status 0 (default 100)
  --output string
      report format: plaintext, json, or yaml (default plaintext)
  --proxy URL
      proxy URL to test, such as https://user:pass@proxy.packetstream.io:31111
  --timeout duration
      time limit for each request, including the proxy connection (default 30s)
```

Exit status is 0 when the success rate is at least `--min-success`, 1 when it
is lower, and 2 for usage errors.

### JSON report

```json
{
  "success": 1,
  "fail": 1,
  "success_rate": 50,
  "average_ttfb": 206,
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

Latencies are in milliseconds. `connect` is the TCP connection to the proxy,
`tls_handshake` adds up the TLS handshakes with an HTTPS proxy and with the
endpoint, `ttfb` runs from the start of the request to the first response
byte, and `total` includes reading up to 1 MiB of the response. Reports show
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

## License

ProxySampler is licensed under the [GNU General Public License v3.0](LICENSE).
