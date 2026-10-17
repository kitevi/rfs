package rfs

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPFetcherFetchesModifiedPageAndCachesValidators(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Last-Modified", "Wed, 07 Apr 1982 00:00:00 GMT")
		_, _ = w.Write([]byte(`<html><body><p>Hello</p></body></html>`))
	}))
	defer server.Close()

	fetcher := NewHTTPFetcher(server.Client())
	result, err := fetcher.Fetch(context.Background(), server.URL, FetchCache{})
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if result.Status != FetchModified {
		t.Fatalf("expected modified status, got %v", result.Status)
	}
	if got := string(result.Page); got != `<html><body><p>Hello</p></body></html>` {
		t.Fatalf("unexpected fetched page: %q", got)
	}
	if result.Cache.ETag != `"v1"` {
		t.Fatalf("unexpected ETag: %q", result.Cache.ETag)
	}
	if result.Cache.LastModified != "Wed, 07 Apr 1982 00:00:00 GMT" {
		t.Fatalf("unexpected Last-Modified: %q", result.Cache.LastModified)
	}
}

func TestHTTPFetcherUsesConditionalHeadersAndReportsNotModified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("If-None-Match"); got != `"v1"` {
			t.Fatalf("If-None-Match = %q", got)
		}
		if got := r.Header.Get("If-Modified-Since"); got != "Wed, 07 Apr 1982 00:00:00 GMT" {
			t.Fatalf("If-Modified-Since = %q", got)
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()

	fetcher := NewHTTPFetcher(server.Client())
	result, err := fetcher.Fetch(context.Background(), server.URL, FetchCache{
		ETag:         `"v1"`,
		LastModified: "Wed, 07 Apr 1982 00:00:00 GMT",
	})
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if result.Status != FetchNotModified {
		t.Fatalf("expected not-modified status, got %v", result.Status)
	}
	if result.Page != nil {
		t.Fatal("expected no page for 304")
	}
}

func TestHTTPFetcherDropsValidatorsWhenModifiedResponseOmitsThem(t *testing.T) {
	var sentConditional []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sentConditional = append(sentConditional, r.Header.Get("If-None-Match"))
		if len(sentConditional) == 1 {
			w.Header().Set("ETag", `"v1"`)
		}
		_, _ = w.Write([]byte("body"))
	}))
	defer server.Close()

	fetcher := NewHTTPFetcher(server.Client())
	first, err := fetcher.Fetch(context.Background(), server.URL, FetchCache{})
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if first.Cache.ETag != `"v1"` {
		t.Fatalf("first fetch ETag = %q", first.Cache.ETag)
	}
	second, err := fetcher.Fetch(context.Background(), server.URL, first.Cache)
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if second.Cache.ETag != "" {
		t.Fatalf("a 200 that sends no validator must clear the stored ETag, got %q", second.Cache.ETag)
	}
	if _, err := fetcher.Fetch(context.Background(), server.URL, second.Cache); err != nil {
		t.Fatalf("third fetch: %v", err)
	}
	if got := sentConditional[len(sentConditional)-1]; got != "" {
		t.Fatalf("third request sent If-None-Match %q after the validator was cleared", got)
	}
}

func TestHTTPFetcherKeepsValidatorsOnNotModified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()

	fetcher := NewHTTPFetcher(server.Client())
	result, err := fetcher.Fetch(context.Background(), server.URL, FetchCache{ETag: `"v1"`, LastModified: "Wed, 07 Apr 1982 00:00:00 GMT"})
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if result.Cache.ETag != `"v1"` || result.Cache.LastModified != "Wed, 07 Apr 1982 00:00:00 GMT" {
		t.Fatalf("304 must preserve the stored validators, got %#v", result.Cache)
	}
}

func TestHTTPFetcherRejectsPagesOverTheBodyLimit(t *testing.T) {
	oversized := bytes.Repeat([]byte("x"), int(maxFetchBodyBytes)+1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(oversized)
	}))
	defer server.Close()

	fetcher := NewHTTPFetcher(server.Client())
	result, err := fetcher.Fetch(context.Background(), server.URL, FetchCache{})
	if err == nil {
		t.Fatalf("fetch of %d bytes succeeded with %d bytes; oversized pages must be rejected", len(oversized), len(result.Page))
	}
	if !strings.Contains(err.Error(), "limit") {
		t.Fatalf("error = %v, want a body-limit rejection", err)
	}
	if result.Page != nil {
		t.Fatalf("rejected fetch returned a page of %d bytes", len(result.Page))
	}
}

func TestHTTPFetcherAcceptsAPageAtTheBodyLimit(t *testing.T) {
	atLimit := bytes.Repeat([]byte("x"), int(maxFetchBodyBytes))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(atLimit)
	}))
	defer server.Close()

	fetcher := NewHTTPFetcher(server.Client())
	result, err := fetcher.Fetch(context.Background(), server.URL, FetchCache{})
	if err != nil {
		t.Fatalf("fetch at the body limit: %v", err)
	}
	if len(result.Page) != len(atLimit) {
		t.Fatalf("page = %d bytes, want %d", len(result.Page), len(atLimit))
	}
}

func TestNewHTTPFetcherUsesAClientWithATimeout(t *testing.T) {
	fetcher := NewHTTPFetcher(nil)
	if fetcher.client == nil {
		t.Fatal("default fetcher has no client")
	}
	if fetcher.client.Timeout <= 0 {
		t.Fatalf("default client timeout = %s, want a bounded request timeout", fetcher.client.Timeout)
	}
}

func TestHTTPFetcherReportsThrottlingWithRetryAfter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	fetcher := NewHTTPFetcher(server.Client())
	result, err := fetcher.Fetch(context.Background(), server.URL, FetchCache{})
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if result.Status != FetchThrottled {
		t.Fatalf("expected throttled status, got %v", result.Status)
	}
	if result.RetryAfter != 2*time.Minute {
		t.Fatalf("unexpected retry-after: %s", result.RetryAfter)
	}
}
