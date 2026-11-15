package rfs

import (
	"bufio"
	"context"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
)

// chromeTransport sends every HTTPS request over a connection opened with
// Chrome's TLS handshake instead of Go's. Some CDNs challenge Go's handshake
// whatever the request carries, and serve the same request once the handshake
// is Chrome's. Only the handshake changes: the request, rfs's User-Agent
// included, goes out as written, over HTTP/2 or HTTP/1.1 as the server
// chooses. See docs/adr/0002-rfs-fetches-flow-extracts.md.
//
// Each request opens its own connection and closes it with the response body.
type chromeTransport struct {
	dialer net.Dialer
	// roots verifies server certificates; nil means the system pool.
	roots *x509.CertPool
	// plain carries http:// requests, which have no handshake to present.
	plain http.RoundTripper
}

func newChromeTransport() *chromeTransport {
	return &chromeTransport{dialer: net.Dialer{Timeout: 30 * time.Second}, plain: http.DefaultTransport}
}

func (t *chromeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" {
		return t.plain.RoundTrip(req)
	}
	conn, err := t.handshake(req.Context(), req.URL)
	if err != nil {
		return nil, err
	}
	if conn.ConnectionState().NegotiatedProtocol == http2.NextProtoTLS {
		return roundTripHTTP2(conn, req)
	}
	return roundTripHTTP1(conn, req)
}

// handshake connects to the request's host and completes Chrome's handshake,
// verifying the server's certificate for that host.
func (t *chromeTransport) handshake(ctx context.Context, target *url.URL) (*utls.UConn, error) {
	host, port := target.Hostname(), target.Port()
	if port == "" {
		port = "443"
	}
	raw, err := t.dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return nil, err
	}
	conn := utls.UClient(raw, &utls.Config{ServerName: host, RootCAs: t.roots}, utls.HelloChrome_Auto)
	if err := conn.HandshakeContext(ctx); err != nil {
		raw.Close()
		return nil, err
	}
	return conn, nil
}

// roundTripHTTP2 sends req on a connection that negotiated HTTP/2.
func roundTripHTTP2(conn net.Conn, req *http.Request) (*http.Response, error) {
	client, err := new(http2.Transport).NewClientConn(conn)
	if err != nil {
		conn.Close()
		return nil, err
	}
	resp, err := client.RoundTrip(req)
	if err != nil {
		client.Close()
		return nil, err
	}
	resp.Body = closeWith{resp.Body, client}
	return resp, nil
}

// roundTripHTTP1 sends req on a connection that negotiated HTTP/1.1, or no
// protocol at all, and asks the server to close it after the response. The
// request's context closes the connection early, which ends a stalled read.
func roundTripHTTP1(conn net.Conn, req *http.Request) (*http.Response, error) {
	stop := context.AfterFunc(req.Context(), func() { conn.Close() })
	release := closerFunc(func() error {
		stop()
		return conn.Close()
	})
	out := req.Clone(req.Context())
	out.Close = true
	if err := out.Write(conn); err != nil {
		release.Close()
		return nil, err
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), out)
	if err != nil {
		release.Close()
		return nil, err
	}
	resp.Body = closeWith{resp.Body, release}
	return resp, nil
}

// closeWith closes the connection under a response once its body is closed.
type closeWith struct {
	io.ReadCloser
	conn io.Closer
}

func (c closeWith) Close() error {
	err := c.ReadCloser.Close()
	c.conn.Close()
	return err
}

type closerFunc func() error

func (f closerFunc) Close() error { return f() }
