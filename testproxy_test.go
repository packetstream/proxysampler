package main

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// testProxy is an in-process HTTP, HTTPS, and SOCKS5 proxy that accepts one
// username and password.
type testProxy struct {
	user, password string
	httpAddr       string
	httpsAddr      string
	socksAddr      string
}

func startTestProxy(t *testing.T, user, password string, cert tls.Certificate) *testProxy {
	t.Helper()
	tp := &testProxy{user: user, password: password}
	tp.httpAddr = tp.listen(t, tp.serveHTTP, nil)
	tp.httpsAddr = tp.listen(t, tp.serveHTTP, &tls.Config{Certificates: []tls.Certificate{cert}})
	tp.socksAddr = tp.listen(t, tp.serveSOCKS5, nil)
	return tp
}

func (tp *testProxy) listen(t *testing.T, handle func(net.Conn), tlsConfig *tls.Config) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if tlsConfig != nil {
		listener = tls.NewListener(listener, tlsConfig)
	}
	t.Cleanup(func() { listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				handle(conn)
			}()
		}
	}()
	return listener.Addr().String()
}

func (tp *testProxy) url(scheme, password string) string {
	addr := map[string]string{"http": tp.httpAddr, "https": tp.httpsAddr, "socks5": tp.socksAddr, "socks5h": tp.socksAddr}[scheme]
	return fmt.Sprintf("%s://%s:%s@%s", scheme, tp.user, password, addr)
}

// serveHTTP handles CONNECT tunnels and absolute-form plain HTTP requests.
func (tp *testProxy) serveHTTP(conn net.Conn) {
	reader := bufio.NewReader(conn)
	req, err := http.ReadRequest(reader)
	if err != nil {
		return
	}

	scheme, encoded, _ := strings.Cut(req.Header.Get("Proxy-Authorization"), " ")
	decoded, _ := base64.StdEncoding.DecodeString(encoded)
	if !strings.EqualFold(scheme, "Basic") || string(decoded) != tp.user+":"+tp.password {
		_, _ = io.WriteString(conn, "HTTP/1.1 407 Proxy Authentication Required\r\n"+
			"Proxy-Authenticate: Basic realm=\"test\"\r\nContent-Length: 0\r\n\r\n")
		return
	}

	if req.Method == http.MethodConnect {
		upstream, err := net.Dial("tcp", req.Host)
		if err != nil {
			_, _ = io.WriteString(conn, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
			return
		}
		defer upstream.Close()
		_, _ = io.WriteString(conn, "HTTP/1.1 200 Connection established\r\n\r\n")
		pipe(conn, reader, upstream)
		return
	}

	req.RequestURI = ""
	req.Header.Del("Proxy-Authorization")
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		_, _ = io.WriteString(conn, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
		return
	}
	defer resp.Body.Close()
	_ = resp.Write(conn)
}

// serveSOCKS5 implements CONNECT with username/password authentication
// (RFC 1928 and RFC 1929).
func (tp *testProxy) serveSOCKS5(conn net.Conn) {
	reader := bufio.NewReader(conn)
	if err := tp.socks5Handshake(conn, reader); err != nil {
		return
	}
	target, err := socks5ReadRequest(reader)
	if err != nil {
		return
	}
	upstream, err := net.Dial("tcp", target)
	if err != nil {
		// 0x04: host unreachable.
		_, _ = conn.Write([]byte{5, 4, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer upstream.Close()
	_, _ = conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
	pipe(conn, reader, upstream)
}

func (tp *testProxy) socks5Handshake(conn net.Conn, reader *bufio.Reader) error {
	header := make([]byte, 2)
	if _, err := io.ReadFull(reader, header); err != nil {
		return err
	}
	methods := make([]byte, header[1])
	if _, err := io.ReadFull(reader, methods); err != nil {
		return err
	}
	if header[0] != 5 || !strings.ContainsRune(string(methods), 2) {
		_, _ = conn.Write([]byte{5, 0xff})
		return errors.New("no username/password authentication")
	}
	if _, err := conn.Write([]byte{5, 2}); err != nil {
		return err
	}

	if version, err := reader.ReadByte(); err != nil || version != 1 {
		return errors.New("bad authentication version")
	}
	user, err := readLengthPrefixed(reader)
	if err != nil {
		return err
	}
	password, err := readLengthPrefixed(reader)
	if err != nil {
		return err
	}
	if user != tp.user || password != tp.password {
		_, _ = conn.Write([]byte{1, 1})
		return errors.New("bad credentials")
	}
	_, err = conn.Write([]byte{1, 0})
	return err
}

func socks5ReadRequest(reader *bufio.Reader) (string, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(reader, header); err != nil {
		return "", err
	}
	if header[1] != 1 {
		return "", fmt.Errorf("unsupported SOCKS command %d", header[1])
	}

	var host string
	switch header[3] {
	case 1, 4:
		addr := make([]byte, map[byte]int{1: net.IPv4len, 4: net.IPv6len}[header[3]])
		if _, err := io.ReadFull(reader, addr); err != nil {
			return "", err
		}
		host = net.IP(addr).String()
	case 3:
		name, err := readLengthPrefixed(reader)
		if err != nil {
			return "", err
		}
		host = name
	default:
		return "", fmt.Errorf("unsupported address type %d", header[3])
	}

	port := make([]byte, 2)
	if _, err := io.ReadFull(reader, port); err != nil {
		return "", err
	}
	return net.JoinHostPort(host, strconv.Itoa(int(port[0])<<8|int(port[1]))), nil
}

func readLengthPrefixed(reader *bufio.Reader) (string, error) {
	length, err := reader.ReadByte()
	if err != nil {
		return "", err
	}
	value := make([]byte, length)
	_, err = io.ReadFull(reader, value)
	return string(value), err
}

// pipe copies in both directions until either side closes. The client side
// reads through reader so bytes it already buffered are not lost.
func pipe(client net.Conn, reader *bufio.Reader, upstream net.Conn) {
	var wg sync.WaitGroup
	wg.Go(func() {
		_, _ = io.Copy(upstream, reader)
		closeWrite(upstream)
	})
	wg.Go(func() {
		_, _ = io.Copy(client, upstream)
		closeWrite(client)
	})
	wg.Wait()
}

func closeWrite(conn net.Conn) {
	if c, ok := conn.(interface{ CloseWrite() error }); ok {
		_ = c.CloseWrite()
	}
}
