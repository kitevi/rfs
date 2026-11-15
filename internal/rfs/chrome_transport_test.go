package rfs

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFetcherSendsRFSRequestsOverChromesHandshake(t *testing.T) {
	for _, protocol := range []int{2, 1} {
		t.Run("HTTP/"+string(rune('0'+protocol)), func(t *testing.T) {
			var gotAgent string
			var gotProto int
			server, hellos := tlsServer(t, protocol == 2, func(w http.ResponseWriter, r *http.Request) {
				gotAgent, gotProto = r.UserAgent(), r.ProtoMajor
				_, _ = w.Write([]byte("page"))
			})
			fetcher := NewHTTPFetcher(&http.Client{Timeout: 10 * time.Second, Transport: &chromeTransport{roots: rootsFor(server)}})
			result, err := fetcher.Fetch(context.Background(), server.URL, FetchCache{})
			if err != nil {
				t.Fatalf("fetch: %v", err)
			}
			if result.Status != FetchModified || string(result.Page) != "page" {
				t.Fatalf("result = %v %q, want the page", result.Status, result.Page)
			}
			if gotProto != protocol {
				t.Fatalf("server saw HTTP/%d, want HTTP/%d", gotProto, protocol)
			}
			if gotAgent != userAgent {
				t.Fatalf("server saw User-Agent %q, want rfs's own %q", gotAgent, userAgent)
			}
			if !hellos.chromeLike() {
				t.Fatal("the ClientHello carried no GREASE cipher suite; Go's own handshake went out instead of Chrome's")
			}
		})
	}
}

// TestHelloLogTellsGoFromChrome is the control for the check above: Go's own
// client reaches the same server without a GREASE suite.
func TestHelloLogTellsGoFromChrome(t *testing.T) {
	server, hellos := tlsServer(t, true, func(w http.ResponseWriter, r *http.Request) {})
	resp, err := server.Client().Get(server.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	resp.Body.Close()
	if hellos.chromeLike() {
		t.Fatal("Go's own ClientHello passed for Chrome's")
	}
}

func TestChromeTransportVerifiesTheServerCertificate(t *testing.T) {
	server, _ := tlsServer(t, true, func(w http.ResponseWriter, r *http.Request) {
		t.Error("a server with an unverifiable certificate received the request")
	})
	client := &http.Client{Timeout: 10 * time.Second, Transport: newChromeTransport()}
	resp, err := client.Get(server.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatal("a self-signed certificate was accepted against the system roots")
	}
	if !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("error = %v, want a certificate rejection", err)
	}
}

func TestChromeTransportStopsAtTheRequestDeadline(t *testing.T) {
	for _, protocol := range []int{2, 1} {
		t.Run("HTTP/"+string(rune('0'+protocol)), func(t *testing.T) {
			server, _ := tlsServer(t, protocol == 2, func(w http.ResponseWriter, r *http.Request) {
				<-r.Context().Done()
			})
			client := &http.Client{Timeout: 300 * time.Millisecond, Transport: &chromeTransport{roots: rootsFor(server)}}
			started := time.Now()
			resp, err := client.Get(server.URL)
			if err == nil {
				resp.Body.Close()
				t.Fatal("a server that never answers produced a response")
			}
			if elapsed := time.Since(started); elapsed > 5*time.Second {
				t.Fatalf("request returned after %s, want it stopped at its 300ms deadline", elapsed)
			}
		})
	}
}

func TestChromeTransportSendsPlainHTTPUnchanged(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("plain"))
	}))
	defer server.Close()
	client := &http.Client{Timeout: 10 * time.Second, Transport: newChromeTransport()}
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if body, _ := io.ReadAll(resp.Body); string(body) != "plain" {
		t.Fatalf("body = %q", body)
	}
}

// tlsServer starts a TLS test server that speaks HTTP/2 or only HTTP/1.1 and
// records the cipher suites each ClientHello offers.
func tlsServer(t *testing.T, http2 bool, handler http.HandlerFunc) (*httptest.Server, *helloLog) {
	t.Helper()
	hellos := &helloLog{}
	server := httptest.NewUnstartedServer(handler)
	server.EnableHTTP2 = http2
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.TLS = &tls.Config{GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		hellos.add(hello.CipherSuites)
		return nil, nil
	}}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server, hellos
}

func rootsFor(server *httptest.Server) *x509.CertPool {
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	return roots
}

// helloLog keeps the cipher suites of every ClientHello a test server saw.
type helloLog struct {
	mu     sync.Mutex
	suites [][]uint16
}

func (l *helloLog) add(suites []uint16) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.suites = append(l.suites, slices.Clone(suites))
}

// chromeLike reports whether a ClientHello arrived and every one offered a
// GREASE cipher suite: Chrome always sends one, Go's own client never does.
func (l *helloLog) chromeLike() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.suites) == 0 {
		return false
	}
	for _, suites := range l.suites {
		if !slices.ContainsFunc(suites, func(suite uint16) bool { return suite&0x0f0f == 0x0a0a }) {
			return false
		}
	}
	return true
}
