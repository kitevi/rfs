package sources_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ppowo/rfs/internal/rfs"
	"github.com/ppowo/rfs/internal/sources"
	"github.com/ppowo/rfs/internal/sources/tplfvg"
	"github.com/ppowo/rfs/internal/sources/trenitaliascioperi"
)

type fixtureFetcher struct {
	routes map[string]rfs.Page
}

func (f *fixtureFetcher) Fetch(_ context.Context, url string, cache rfs.FetchCache) (rfs.FetchResult, error) {
	page, ok := f.routes[url]
	if !ok {
		return rfs.FetchResult{}, fmt.Errorf("unexpected fetch %s", url)
	}
	return rfs.FetchResult{Status: rfs.FetchModified, Page: page, Cache: cache}, nil
}

type feedClock struct{ now time.Time }

func (c feedClock) Now() time.Time { return c.now }

func fixture(t *testing.T, name string) rfs.Page {
	t.Helper()
	data, err := os.ReadFile(filepath.FromSlash(name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return rfs.Page(data)
}

func sourceByID(t *testing.T, id string) rfs.Source {
	t.Helper()
	for _, source := range sources.All() {
		if source.ID == id {
			return source
		}
	}
	t.Fatalf("source %q is not registered", id)
	return rfs.Source{}
}

// pollIntoStore runs one fixture-fed poll against the given store and returns
// the feed items it produced.
func pollIntoStore(t *testing.T, store *rfs.SQLiteStore, source rfs.Source, routes map[string]rfs.Page) []rfs.Item {
	t.Helper()
	poller := rfs.Poller{
		Fetcher: &fixtureFetcher{routes: routes},
		Store:   store,
		Clock:   feedClock{now: time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)},
	}
	if _, err := poller.Poll(context.Background(), source); err != nil {
		t.Fatalf("poll %s: %v", source.ID, err)
	}
	items, err := store.LoadSnapshot(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	return items
}

// pollFixture runs one fixture-fed poll against a temporary SQLite database.
func pollFixture(t *testing.T, source rfs.Source, routes map[string]rfs.Page) []rfs.Item {
	t.Helper()
	store, err := rfs.OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return pollIntoStore(t, store, source, routes)
}

// TestStrikeFeedEndpointsRender serves both strike feeds through the HTTP
// handler, so the RSS and HTML routes subscribers use are exercised end to end.
func TestStrikeFeedEndpointsRender(t *testing.T) {
	store, err := rfs.OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	pollIntoStore(t, store, sourceByID(t, "tpl-fvg-scioperi"), map[string]rfs.Page{
		tplfvg.PageURL: fixture(t, "tplfvg/testdata/scioperi_index.html"),
		"https://tplfvg.it/it/servizi/scioperi/10set26/": fixture(t, "tplfvg/testdata/sciopero_udine_10set26.html"),
	})
	pollIntoStore(t, store, sourceByID(t, "trenitalia-scioperi"), map[string]rfs.Page{
		trenitaliascioperi.PageURL: fixture(t, "trenitaliascioperi/testdata/notizie_20260905.html"),
	})

	handler := rfs.NewHTTPHandler(store, sources.All(), rfs.BuildInfo{})
	cases := []struct {
		path string
		want string
	}{
		{"/feeds/tpl-fvg-scioperi.xml", "10set26"},
		{"/feeds/tpl-fvg-scioperi.html", "10set26"},
		{"/feeds/trenitalia-scioperi.xml", "sciopero nazionale"},
		{"/feeds/trenitalia-scioperi.html", "sciopero nazionale"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", recorder.Code)
			}
			if !strings.Contains(recorder.Body.String(), tc.want) {
				t.Fatalf("%s body does not contain %q: %.300s", tc.path, tc.want, recorder.Body.String())
			}
		})
	}
}

func renderBoth(t *testing.T, source rfs.Source, items []rfs.Item) (rss string, html string) {
	t.Helper()
	rssBytes, err := rfs.RenderRSS(source.Meta, items)
	if err != nil {
		t.Fatalf("render RSS: %v", err)
	}
	htmlBytes, err := rfs.RenderHTMLFeed(source.ID, source.Meta, items, rfs.BuildInfo{})
	if err != nil {
		t.Fatalf("render HTML feed: %v", err)
	}
	return string(rssBytes), string(htmlBytes)
}

func TestBusFeedPublishesActiveNoticeOnFirstRun(t *testing.T) {
	source := sourceByID(t, "tpl-fvg-scioperi")
	detail := "tplfvg/testdata/sciopero_udine_10set26.html"
	items := pollFixture(t, source, map[string]rfs.Page{
		tplfvg.PageURL: fixture(t, "tplfvg/testdata/scioperi_index.html"),
		"https://tplfvg.it/it/servizi/scioperi/10set26/": fixture(t, detail),
	})
	if len(items) != 1 {
		t.Fatalf("first run emitted %d items, want the notice in force", len(items))
	}
	if items[0].GUID != "tpl-fvg-scioperi:1:https://tplfvg.it/it/servizi/scioperi/10set26/" {
		t.Fatalf("GUID = %q, want a revision-qualified permalink", items[0].GUID)
	}
	if !strings.HasPrefix(items[0].Title, "[Bus · Arriva Udine] ") {
		t.Fatalf("title = %q, want the Arriva Udine notice", items[0].Title)
	}
	rss, html := renderBoth(t, source, items)
	for _, document := range []struct {
		name string
		body string
	}{
		{"RSS", rss},
		{"HTML", html},
	} {
		if !strings.Contains(document.body, "10set26") {
			t.Fatalf("%s output does not link the notice: %.400s", document.name, document.body)
		}
		if !strings.Contains(document.body, "Possibili cancellazioni") {
			t.Fatalf("%s output lost the notice summary: %.400s", document.name, document.body)
		}
	}
}

func TestBusFeedEscapesUpstreamText(t *testing.T) {
	source := sourceByID(t, "tpl-fvg-scioperi")
	index := fixture(t, "tplfvg/testdata/scioperi_index.html")
	detail := string(fixture(t, "tplfvg/testdata/sciopero_udine_10set26.html"))
	detail = strings.Replace(detail,
		"Possibili cancellazioni e ritardi su tutta la rete",
		"Possibili cancellazioni &lt;script&gt;alert(1)&lt;/script&gt; su tutta la rete", 1)
	items := pollFixture(t, source, map[string]rfs.Page{
		tplfvg.PageURL: index,
		"https://tplfvg.it/it/servizi/scioperi/10set26/": rfs.Page(detail),
	})
	if len(items) != 1 {
		t.Fatalf("emitted %d items, want 1", len(items))
	}
	if !strings.Contains(items[0].Description, "<script>alert(1)</script>") {
		t.Fatalf("test fixture did not carry the injection text: %q", items[0].Description)
	}
	rss, html := renderBoth(t, source, items)
	for _, document := range []struct {
		name string
		body string
	}{
		{"RSS", rss},
		{"HTML", html},
	} {
		if strings.Contains(document.body, "<script>alert(1)</script>") {
			t.Fatalf("%s output rendered upstream text as markup", document.name)
		}
		if !strings.Contains(document.body, "&lt;script&gt;alert(1)&lt;/script&gt;") {
			t.Fatalf("%s output dropped or re-encoded the notice text: %.400s", document.name, document.body)
		}
	}
}

func TestRailFeedPublishesArchivedNationalStrike(t *testing.T) {
	source := sourceByID(t, "trenitalia-scioperi")
	items := pollFixture(t, source, map[string]rfs.Page{
		trenitaliascioperi.PageURL: fixture(t, "trenitaliascioperi/testdata/notizie_20260905.html"),
	})
	if len(items) != 1 {
		t.Fatalf("first run emitted %d items, want the national notice", len(items))
	}
	wantGUID := "trenitalia-scioperi:1:" + trenitaliascioperi.HumanURL + "#infomobility_summary_548526369"
	if items[0].GUID != wantGUID {
		t.Fatalf("GUID = %q, want %q", items[0].GUID, wantGUID)
	}
	if !strings.HasPrefix(items[0].Title, "[Treni · Nazionale] ") {
		t.Fatalf("title = %q, want a national scope label", items[0].Title)
	}
	rss, html := renderBoth(t, source, items)
	if !strings.Contains(rss, "Infomobilità") && !strings.Contains(rss, "sciopero nazionale") {
		t.Fatalf("RSS output lost the notice: %.400s", rss)
	}
	if !strings.Contains(html, "sciopero nazionale") {
		t.Fatalf("HTML output lost the notice: %.400s", html)
	}
}
