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
	"github.com/ppowo/rfs/internal/sources/aptgorizia"
	"github.com/ppowo/rfs/internal/sources/arrivaudine"
	"github.com/ppowo/rfs/internal/sources/trenitalia"
	"github.com/ppowo/rfs/internal/sources/triestetrasporti"
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

// TestOperatorFeedEndpointsRender serves both operator feeds through the HTTP
// handler, so the RSS and HTML routes subscribers use are exercised end to end.
func TestOperatorFeedEndpointsRender(t *testing.T) {
	store, err := rfs.OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	pollIntoStore(t, store, sourceByID(t, "trenitalia-disruptions"), map[string]rfs.Page{
		trenitalia.PageURL: fixture(t, "trenitalia/testdata/notizie_20260905.html"),
	})

	handler := rfs.NewHTTPHandler(store, sources.All(), rfs.BuildInfo{})
	cases := []struct {
		path string
		want string
	}{
		{"/feeds/trenitalia-disruptions.xml", "sciopero nazionale"},
		{"/feeds/trenitalia-disruptions.html", "sciopero nazionale"},
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

// TestPerOperatorFeedsPublishTheirOwnNotices polls each operator feed with its
// captured page and serves the result through the HTTP handler, so the feed a
// subscriber picks per operator is exercised end to end.
func TestPerOperatorFeedsPublishTheirOwnNotices(t *testing.T) {
	store, err := rfs.OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	cases := []struct {
		id      string
		url     string
		fixture string
		label   string
		notice  string
		count   int
	}{
		{"trieste-trasporti", triestetrasporti.PageURL, "triestetrasporti/testdata/avvisi_20260910.html", "[Bus · Trieste Trasporti] ", "Maltempo, tutte le deviazioni in vigore", 9},
		{"arriva-udine", arrivaudine.PageURL, "arrivaudine/testdata/notices_20260910.json", "[Bus · Arriva Udine] ", "Avviso di sciopero di 4 ore per il giorno 10 settembre 2026", 10},
		{"apt-gorizia", aptgorizia.PageURL, "aptgorizia/testdata/avvisi_20260910.html", "[Bus · APT Gorizia] ", "Moraro, fermate sospese per processione il 08/09/2026", 6},
	}
	handler := rfs.NewHTTPHandler(store, sources.All(), rfs.BuildInfo{})
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			source := sourceByID(t, tc.id)
			items := pollIntoStore(t, store, source, map[string]rfs.Page{tc.url: fixture(t, tc.fixture)})
			if len(items) != tc.count {
				t.Fatalf("first run emitted %d items, want %d", len(items), tc.count)
			}
			found := false
			for _, item := range items {
				if !strings.HasPrefix(item.Title, tc.label) {
					t.Fatalf("title = %q, want the %q label", item.Title, tc.label)
				}
				if strings.Contains(item.Title, tc.notice) {
					found = true
				}
			}
			if !found {
				t.Fatalf("no item carries %q", tc.notice)
			}
			for _, path := range []string{"/feeds/" + tc.id + ".xml", "/feeds/" + tc.id + ".html"} {
				recorder := httptest.NewRecorder()
				handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
				if recorder.Code != http.StatusOK {
					t.Fatalf("%s status = %d, want 200", path, recorder.Code)
				}
				if !strings.Contains(recorder.Body.String(), tc.notice) {
					t.Fatalf("%s body does not carry %q", path, tc.notice)
				}
				if strings.Contains(recorder.Body.String(), "<script") {
					t.Fatalf("%s rendered markup from upstream text", path)
				}
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

func TestRailFeedHandsExistingSubscribersTheBroaderScope(t *testing.T) {
	ctx := context.Background()
	store, err := rfs.OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	source := sourceByID(t, "trenitalia-disruptions")
	page := fixture(t, "trenitalia/testdata/notizie_20260905.html")
	extracted, err := trenitalia.Flow{}.Extract(page)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	strikeGUID := trenitalia.HumanURL + "#infomobility_summary_548526369"
	var legacy []rfs.ExtractedItem
	for _, item := range extracted {
		if item.GUID == strikeGUID {
			legacy = append(legacy, item)
		}
	}
	if len(legacy) != 1 {
		t.Fatalf("fixture no longer carries the strike notice: %#v", extracted)
	}
	// An earlier build stored strike notices only, at extraction version 1.
	if err := store.SaveChanges(ctx, source.ID, rfs.ChangeState{Items: legacy, Version: 1, Revision: 1, Initialized: true}, nil, rfs.FetchCache{ExtractVersion: 1}); err != nil {
		t.Fatalf("seed legacy baseline: %v", err)
	}

	items := pollIntoStore(t, store, source, map[string]rfs.Page{trenitalia.PageURL: page})
	if len(items) != 1 {
		t.Fatalf("upgrade emitted %#v, want only the newly covered works page", items)
	}
	want := "trenitalia-disruptions:2:" + trenitalia.HumanURL + "#infomobility_summary_1720830883"
	if items[0].GUID != want {
		t.Fatalf("GUID = %q, want %q", items[0].GUID, want)
	}
	if !strings.HasPrefix(items[0].Title, "[Treni · FVG] ") {
		t.Fatalf("title = %q, want an FVG scope label", items[0].Title)
	}
}

func TestRailFeedPublishesArchivedStrikeAndFVGDisruptions(t *testing.T) {
	source := sourceByID(t, "trenitalia-disruptions")
	items := pollFixture(t, source, map[string]rfs.Page{
		trenitalia.PageURL: fixture(t, "trenitalia/testdata/notizie_20260905.html"),
	})
	if len(items) != 2 {
		t.Fatalf("first run emitted %d items, want the national strike and the FVG works page", len(items))
	}
	titles := map[string]string{}
	for _, item := range items {
		titles[item.GUID] = item.Title
	}
	strike := "trenitalia-disruptions:1:" + trenitalia.HumanURL + "#infomobility_summary_548526369"
	if title := titles[strike]; !strings.HasPrefix(title, "[Treni · Nazionale] ") {
		t.Fatalf("strike title = %q, want a national scope label", title)
	}
	works := "trenitalia-disruptions:1:" + trenitalia.HumanURL + "#infomobility_summary_1720830883"
	if title := titles[works]; !strings.HasPrefix(title, "[Treni · FVG] ") {
		t.Fatalf("works title = %q, want an FVG scope label", title)
	}
	rss, html := renderBoth(t, source, items)
	for _, document := range []struct {
		name string
		body string
	}{
		{"RSS", rss},
		{"HTML", html},
	} {
		if !strings.Contains(document.body, "sciopero nazionale") {
			t.Fatalf("%s output lost the strike notice: %.400s", document.name, document.body)
		}
		if !strings.Contains(document.body, "INFOLAVORI FRIULI VENEZIA GIULIA") {
			t.Fatalf("%s output lost the works page: %.400s", document.name, document.body)
		}
	}
}
