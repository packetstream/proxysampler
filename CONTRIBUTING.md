# Contributing

Contributions are welcome, from bug reports to new features.

> [!CAUTION]
> This repository is public. Never commit a username, auth key, or proxy file with real credentials. If you expose a key, rotate it in the [dashboard](https://app.packetstream.io/dashboard/network_access) straight away.

## Development

You need [Go](https://go.dev/dl/) 1.26 or newer and [golangci-lint](https://golangci-lint.run/) v2.

```bash
go test -race ./...
golangci-lint run ./...
golangci-lint fmt --diff ./...
```

The tests run against an in-process HTTP, HTTPS, and SOCKS5 proxy, so they need no network access or credentials. Add a test for each bug fix and feature.

Keep these properties:

- A request counts as a success only when the endpoint answers with an expected status.
- Reports, errors, and logs never contain a proxy password. Use `redactProxy` and `redactError`.
- Each request uses a new connection and ignores `HTTP_PROXY` and similar variables.
- TLS certificate verification stays on.
- Flags and report fields stay backward compatible within a major version.

## Pull requests

Describe what the change fixes or adds, and how you tested it. Feel free to include your [referral link](https://app.packetstream.io/dashboard/referrals) if you would like to be credited for your contribution.
