package rfs

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var testBuildInfo = BuildInfo{
	Version:    "test-1.2.3",
	Commit:     "abc1234",
	CommitDate: "2026-07-09",
	BuildDate:  "2026-07-09",
	GoVersion:  "go1.24.5",
	GOOS:       "linux",
	GOARCH:     "amd64",
}

func TestHTTPHandlerServesStoredSnapshotAsRSS(t *testing.T) {
	store, err := OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	pubDate := time.Date(1982, 4, 7, 0, 0, 0, 0, time.UTC)
	if err := store.SaveSnapshot(t.Context(), "meltzer", []Item{{
		GUID:        "meltzer:1982-04-07:ric-flair-vs-butch-reed:miami-beach-show",
		Title:       "Ric Flair vs. Butch Reed — Miami Beach show",
		Link:        "https://example.com/meltzer",
		Description: "CWF · 5 stars",
		PubDate:     pubDate,
	}}); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}

	handler := NewHTTPHandler(store, []Source{{
		ID: "meltzer",
		Meta: SourceMeta{
			Title:       "Meltzer 5-star matches",
			Description: "Matches rated 5 or more stars by Dave Meltzer.",
			Link:        "https://example.com/meltzer",
		},
	}}, testBuildInfo)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/feeds/meltzer.xml", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/rss+xml") {
		t.Fatalf("Content-Type = %q", got)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `<title>Meltzer 5-star matches</title>`) {
		t.Fatalf("missing channel title:\n%s", body)
	}
	if !strings.Contains(body, `<guid isPermaLink="false">meltzer:1982-04-07:ric-flair-vs-butch-reed:miami-beach-show</guid>`) {
		t.Fatalf("missing item GUID:\n%s", body)
	}
}

func TestHTTPHandlerReturns404ForUnknownFeed(t *testing.T) {
	store, err := OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	handler := NewHTTPHandler(store, []Source{{ID: "meltzer", Meta: SourceMeta{Title: "Meltzer"}}}, testBuildInfo)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/feeds/unknown.xml", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestHTTPHandlerServesIndexPage(t *testing.T) {
	store, err := OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	handler := NewHTTPHandler(store, []Source{{
		ID:   "meltzer",
		Meta: SourceMeta{Title: "Meltzer 5-star matches", Description: "Rated matches."},
	}}, testBuildInfo)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("Content-Type = %q", got)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "Meltzer 5-star matches") {
		t.Fatalf("missing source title in index:\n%s", body)
	}
	if !strings.Contains(body, `href="/feeds/meltzer.html"`) {
		t.Fatalf("missing link to HTML view:\n%s", body)
	}
	if !strings.Contains(body, `href="/feeds/meltzer.xml"`) {
		t.Fatalf("missing link to RSS feed:\n%s", body)
	}
	if !strings.Contains(body, "test-1.2.3") {
		t.Fatalf("missing build info in index:\n%s", body)
	}
	if !strings.Contains(body, `<footer class="version">`) {
		t.Fatalf("missing version footer:\n%s", body)
	}
}

func TestHTTPHandlerServesFeedAsHTML(t *testing.T) {
	store, err := OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	pubDate := time.Date(1982, 4, 7, 0, 0, 0, 0, time.UTC)
	if err := store.SaveSnapshot(t.Context(), "meltzer", []Item{{
		GUID:        "meltzer:1982-04-07:ric-flair-vs-butch-reed:miami-beach-show",
		Title:       "Ric Flair vs. Butch Reed — Miami Beach show",
		Link:        "https://example.com/meltzer",
		Description: "CWF · 5 stars",
		PubDate:     pubDate,
	}}); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}

	handler := NewHTTPHandler(store, []Source{{
		ID:   "meltzer",
		Meta: SourceMeta{Title: "Meltzer 5-star matches", Link: "https://example.com/meltzer"},
	}}, testBuildInfo)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/feeds/meltzer.html", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("Content-Type = %q", got)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "Ric Flair vs. Butch Reed — Miami Beach show") {
		t.Fatalf("missing item title:\n%s", body)
	}
	if !strings.Contains(body, "7 April 1982") {
		t.Fatalf("missing formatted date:\n%s", body)
	}
	if !strings.Contains(body, "CWF · 5 stars") {
		t.Fatalf("missing description:\n%s", body)
	}
	if !strings.Contains(body, "test-1.2.3") {
		t.Fatalf("missing build info in feed:\n%s", body)
	}
	if !strings.Contains(body, `<footer class="version">`) {
		t.Fatalf("missing version footer:\n%s", body)
	}
}

func TestHTTPHandlerReturns404ForUnknownFeedFormat(t *testing.T) {
	store, err := OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	handler := NewHTTPHandler(store, []Source{{
		ID:   "meltzer",
		Meta: SourceMeta{Title: "Meltzer 5-star matches"},
	}}, testBuildInfo)

	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/feeds/meltzer.json", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s; want 404 for unknown format", recorder.Code, recorder.Body.String())
	}
}
