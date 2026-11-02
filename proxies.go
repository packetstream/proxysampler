package main

import (
	"bufio"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
)

// loadProxies returns the proxies chosen with --proxy or --file, or the
// PacketStream proxy.
func loadProxies(cfg config) ([]*url.URL, error) {
	if cfg.singleProxy == "" && cfg.proxyFile == "" {
		p, err := packetStreamProxy(cfg)
		if err != nil {
			return nil, err
		}
		return []*url.URL{p}, nil
	}
	if cfg.singleProxy != "" {
		p, err := parseProxy(cfg.singleProxy)
		if err != nil {
			return nil, err
		}
		return []*url.URL{p}, nil
	}

	file, err := os.Open(cfg.proxyFile)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var proxies []*url.URL
	scanner := bufio.NewScanner(file)
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		p, err := parseProxy(text)
		if err != nil {
			return nil, fmt.Errorf("%s line %d: %w", cfg.proxyFile, line, err)
		}
		proxies = append(proxies, p)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(proxies) == 0 {
		return nil, fmt.Errorf("%s has no proxies", cfg.proxyFile)
	}
	return proxies, nil
}

// parseProxy validates a proxy URL. Errors never include the URL, which may
// contain a password.
func parseProxy(raw string) (*url.URL, error) {
	p, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("invalid proxy URL")
	}
	switch p.Scheme {
	case "http", "https", "socks5", "socks5h":
	case "":
		return nil, errors.New("proxy URL needs a scheme, such as https://")
	default:
		return nil, fmt.Errorf("unsupported proxy scheme %q: use http, https, socks5, or socks5h", p.Scheme)
	}
	if p.Hostname() == "" || p.Port() == "" {
		return nil, errors.New("proxy URL needs a host and port")
	}
	return p, nil
}

func checkEndpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("invalid --endpoint %q: use an http or https URL", raw)
	}
	return nil
}

// PacketStream passwords carry targeting options as suffixes on the auth
// key, such as _country-US or _session-abc. Redaction keeps them.
var passwordModifiers = regexp.MustCompile(`(?:_(?:country|session)-[A-Za-z0-9-]+)+$`)

const redacted = "xxxxx"

// redactProxy returns the proxy URL with its password hidden.
func redactProxy(p *url.URL) string {
	password, ok := p.User.Password()
	if !ok {
		return p.String()
	}
	clean := *p
	clean.User = url.UserPassword(p.User.Username(), redacted+passwordModifiers.FindString(password))
	return clean.String()
}

// redactError removes the proxy password from an error message that quotes
// the proxy URL.
func redactError(err error, p *url.URL) string {
	msg := err.Error()
	if password, ok := p.User.Password(); ok && password != "" {
		for _, form := range []string{password, url.QueryEscape(password), url.PathEscape(password)} {
			msg = strings.ReplaceAll(msg, ":"+form+"@", ":"+redacted+"@")
		}
	}
	return msg
}
