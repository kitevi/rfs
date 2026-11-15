// Package rfstest holds the helpers Flow tests share: an in-memory store, a
// feed reader, a fixture loader and a live page fetch through rfs's client.
// It is imported only from tests; rfs's own tests cannot use it because it
// imports rfs.
package rfstest

import (
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/kitevi/rfs/internal/rfs"
)

// Store opens an in-memory SQLite store for one test and closes it when the
// test ends.
func Store(t testing.TB) *rfs.SQLiteStore {
	t.Helper()
	store, err := rfs.OpenSQLiteStore(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// FeedItem is the part of an RSS item the Flow tests read back.
type FeedItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
}

// Feed serves one source's feed from the test store and returns its items.
func Feed(t testing.TB, store *rfs.SQLiteStore, source rfs.Source) []FeedItem {
	t.Helper()
	w := httptest.NewRecorder()
	rfs.NewHTTPHandler(store, []rfs.Source{source}, rfs.BuildInfo{}).
		ServeHTTP(w, httptest.NewRequest("GET", "/feeds/"+source.ID+".xml", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("feed status %d: %s", w.Code, w.Body.String())
	}
	var doc struct {
		Items []FeedItem `xml:"channel>item"`
	}
	if err := xml.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Items
}

// Fixture reads one file from the calling package's testdata directory.
func Fixture(t testing.TB, name string) rfs.Page {
	t.Helper()
	page, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return page
}

// FetchLive fetches one upstream page for an opt-in live probe through the
// client rfs polls with, so a probe passes only where rfs itself would.
func FetchLive(t testing.TB, pageURL string) rfs.Page {
	t.Helper()
	result, err := rfs.NewHTTPFetcher(nil).Fetch(t.Context(), pageURL, rfs.FetchCache{})
	if err != nil {
		t.Fatalf("fetch %s: %v", pageURL, err)
	}
	if result.Status != rfs.FetchModified {
		t.Fatalf("fetch %s: status %d, want the page", pageURL, result.Status)
	}
	return result.Page
}
