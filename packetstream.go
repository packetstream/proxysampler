package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
)

var (
	countryCode = regexp.MustCompile(`^[A-Za-z]{2}$`)
	sessionName = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

	// countryModifier finds the exit country requested in a PacketStream
	// password, such as KEY_country-US_session-abc.
	countryModifier = regexp.MustCompile(`_country-([A-Za-z]{2})(?:_|$)`)
)

// PacketStream proxy ports by protocol.
var packetStreamPorts = map[string]string{"https": "31111", "http": "31112", "socks5": "31113"}

// checkPacketStreamFlags rejects PacketStream options that would be ignored
// or produce an invalid password.
func checkPacketStreamFlags(cfg config, fs *flag.FlagSet) error {
	if cfg.proxyFile != "" || cfg.singleProxy != "" {
		var err error
		fs.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "protocol", "country", "session", "host":
				err = fmt.Errorf("--%s only applies without --proxy or --file; put options in the proxy URL instead", f.Name)
			}
		})
		return err
	}

	switch {
	case packetStreamPorts[cfg.protocol] == "":
		return fmt.Errorf("invalid --protocol %q: use https, http, or socks5", cfg.protocol)
	case cfg.country != "" && !countryCode.MatchString(cfg.country):
		return fmt.Errorf("invalid --country %q: use a two-letter code such as US", cfg.country)
	case cfg.session != "" && !sessionName.MatchString(cfg.session):
		return fmt.Errorf("invalid --session %q: use letters, digits, and hyphens", cfg.session)
	case cfg.host == "":
		return errors.New("--host must not be empty")
	}
	return nil
}

// packetStreamProxy builds a proxy URL from the PACKETSTREAM_USER and
// PACKETSTREAM_AUTH_KEY environment variables, which keeps the auth key out
// of the command line.
func packetStreamProxy(cfg config) (*url.URL, error) {
	user, key := os.Getenv("PACKETSTREAM_USER"), os.Getenv("PACKETSTREAM_AUTH_KEY")
	if user == "" || key == "" {
		return nil, errors.New("use --proxy or --file, or set PACKETSTREAM_USER and PACKETSTREAM_AUTH_KEY to test PacketStream")
	}

	// Targeting options are suffixes on the auth key.
	password := key
	if cfg.country != "" {
		password += "_country-" + strings.ToUpper(cfg.country)
	}
	if cfg.session != "" {
		password += "_session-" + cfg.session
	}

	return &url.URL{
		Scheme: cfg.protocol,
		User:   url.UserPassword(user, password),
		Host:   net.JoinHostPort(cfg.host, packetStreamPorts[cfg.protocol]),
	}, nil
}

// requestedCountry returns the exit country requested in a proxy password.
func requestedCountry(p *url.URL) string {
	password, _ := p.User.Password()
	if m := countryModifier.FindStringSubmatch(password); m != nil {
		return strings.ToUpper(m[1])
	}
	return ""
}

// hasSession reports whether a proxy password asks for a sticky session.
func hasSession(p *url.URL) bool {
	password, _ := p.User.Password()
	return strings.Contains(password, "_session-")
}
