package sources_test

import (
	"context"
	"encoding/json"
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
	"github.com/ppowo/rfs/internal/sources/notices"
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

// capturedAt is the instant every fixture in this package was captured at.
func capturedAt() time.Time { return time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC) }

// pollIntoStore runs one fixture-fed poll against the given store and returns
// the feed items it produced.
func pollIntoStore(t *testing.T, store *rfs.SQLiteStore, source rfs.Source, routes map[string]rfs.Page) []rfs.Item {
	t.Helper()
	return pollIntoStoreAt(t, store, source, routes, capturedAt())
}

// pollIntoStoreAt runs one fixture-fed poll at a chosen instant.
func pollIntoStoreAt(t *testing.T, store *rfs.SQLiteStore, source rfs.Source, routes map[string]rfs.Page, at time.Time) []rfs.Item {
	t.Helper()
	poller := rfs.Poller{
		Fetcher: &fixtureFetcher{routes: routes},
		Store:   store,
		Clock:   feedClock{now: at},
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
		{"arriva-udine", arrivaudine.PageURL, "arrivaudine/testdata/notices_20260910.json", "[Bus · Arriva Udine] ", "Avviso di sciopero di 4 ore per il giorno 10 settembre 2026", 2},
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

// TestBusFeedsAnnounceOnlyWhatIsStillLive drives the captured operator pages
// through the poller at fixed instants, so the age cutoff and the stated windows
// are exercised end to end rather than one notice at a time.
func TestBusFeedsAnnounceOnlyWhatIsStillLive(t *testing.T) {
	ctx := context.Background()
	store, err := rfs.OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	source := sourceByID(t, "arriva-udine")
	routes := map[string]rfs.Page{arrivaudine.PageURL: fixture(t, "arrivaudine/testdata/notices_20260910.json")}

	// The captured page lists 10 notices. Two are news on 10 September: the
	// strike announced for that same day, and the timetable notice published on
	// 26 August. The rest are older than two calendar months.
	items := pollIntoStore(t, store, source, routes)
	if len(items) != 2 {
		t.Fatalf("first run emitted %#v, want the two notices inside the freshness window", items)
	}
	titles := map[string]string{}
	for _, item := range items {
		titles[item.Title] = item.GUID
	}
	for _, want := range []string{"per il giorno 10 settembre 2026", "nuovi orari"} {
		found := false
		for title := range titles {
			if strings.Contains(title, want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("emitted %#v, missing a notice carrying %q", titles, want)
		}
	}
	for title := range titles {
		if strings.Contains(title, "pasquale") {
			t.Fatalf("the March Easter notice was announced on 10 September: %#v", titles)
		}
	}

	// Every observed notice stays in the baseline, announced or not, so a poll
	// that only re-reads the same page cannot mistake one of them for news.
	state, err := store.LoadChangeState(ctx, source.ID)
	if err != nil {
		t.Fatalf("load change state: %v", err)
	}
	if len(state.Items) != 10 {
		t.Fatalf("baseline holds %d notices, want all 10 the page listed", len(state.Items))
	}
	baseline := map[string]bool{}
	for _, item := range state.Items {
		baseline[item.GUID] = true
	}
	if !baseline["https://www.arrivaudine.it/notice/orari-dei-servizi-nel-periodo-pasquale-3/"] {
		t.Fatalf("the suppressed Easter notice is not in the baseline: %#v", baseline)
	}

	// A restart re-reads the same page through a new poller and must not
	// re-announce anything it has already seen, suppressed notices included.
	restarted := rfs.Poller{Fetcher: &fixtureFetcher{routes: routes}, Store: store, Clock: feedClock{now: capturedAt()}}
	if _, err := restarted.Poll(ctx, source); err != nil {
		t.Fatalf("restart poll: %v", err)
	}
	afterRestart, err := store.LoadSnapshot(ctx, source.ID)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if len(afterRestart) != len(items) {
		t.Fatalf("restart emitted %d items, want the %d already published", len(afterRestart), len(items))
	}

	// The strike notice carries its own publication time, not the observation
	// time, and the feed shows it. 10:06 in Rome is 08:06 UTC.
	strike := items[0]
	if !strings.Contains(strike.Description, "Pubblicato: 04/09/2026 10:06") {
		t.Fatalf("description = %q, want the notice's own publication date", strike.Description)
	}
	if want := time.Date(2026, time.September, 4, 8, 6, 44, 0, time.UTC); !strike.PubDate.Equal(want) {
		t.Fatalf("pubDate = %v, want the notice's own publication time %v", strike.PubDate, want)
	}
	rss, html := renderBoth(t, source, items)
	for name, body := range map[string]string{"RSS": rss, "HTML": html} {
		if !strings.Contains(body, "04 Sep 2026") && name == "RSS" {
			t.Fatalf("%s lost the publication timestamp: %.600s", name, body)
		}
		if !strings.Contains(body, "Pubblicato: 04/09/2026 10:06") {
			t.Fatalf("%s lost the published date: %.600s", name, body)
		}
	}

	// The next day the strike day is over. The same page now yields only the
	// timetable notice: a notice whose stated day has passed is not announced,
	// and the reader keeps the entry it was already sent.
	nextDay := time.Date(2026, time.September, 11, 8, 0, 0, 0, time.UTC)
	later, err := rfs.OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { later.Close() })
	after := pollIntoStoreAt(t, later, source, routes, nextDay)
	if len(after) != 1 || !strings.Contains(after[0].Title, "nuovi orari") {
		t.Fatalf("the day after emitted %#v, want only the still-open timetable notice", after)
	}

	// Polling the already-seen page again adds no revision either.
	items = pollIntoStoreAt(t, store, source, routes, nextDay)
	for _, item := range items {
		if strings.HasPrefix(item.GUID, "arriva-udine:2:") {
			t.Fatalf("an ended notice was announced again on a later poll: %#v", item)
		}
	}
	if len(items) != 2 {
		t.Fatalf("a later poll changed the feed to %#v, want no new announcement", items)
	}
}

// TestBusFeedsKeepNoticesTheCollectionCannotDate covers the operator pages that
// state no publication date: a start date or a bare date must never be turned
// into the age that would suppress the notice.
func TestBusFeedsKeepNoticesTheCollectionCannotDate(t *testing.T) {
	store, err := rfs.OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	cases := []struct {
		id      string
		url     string
		fixture string
		notice  string
	}{
		{"apt-gorizia", aptgorizia.PageURL, "aptgorizia/testdata/avvisi_20260910.html", "fermata sospesa dal 29-06-2026"},
		{"trieste-trasporti", triestetrasporti.PageURL, "triestetrasporti/testdata/avvisi_20260910.html", "Chiusura di via Sant'Anastasio"},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			source := sourceByID(t, tc.id)
			items := pollIntoStore(t, store, source, map[string]rfs.Page{tc.url: fixture(t, tc.fixture)})
			for _, item := range items {
				if strings.Contains(item.Title, tc.notice) {
					return
				}
			}
			t.Fatalf("suppressed %q, which the page still lists in force: %#v", tc.notice, items)
		})
	}
}

// TestBusFeedUpgradeRebaselinesWithoutReplayingTheArchive pins the upgrade path.
// The stored payload changed shape with the extraction version, so a baseline
// written by the older build must be replaced in silence instead of being
// decoded with the new decoder or replayed to subscribers.
func TestBusFeedUpgradeRebaselinesWithoutReplayingTheArchive(t *testing.T) {
	ctx := context.Background()
	store, err := rfs.OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	source := sourceByID(t, "arriva-udine")
	routes := map[string]rfs.Page{arrivaudine.PageURL: fixture(t, "arrivaudine/testdata/notices_20260910.json")}

	list, err := arrivaudine.ParseNotices(routes[arrivaudine.PageURL])
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	// Version 1 stored a single free-text date field.
	var legacy []rfs.ExtractedItem
	for _, notice := range list {
		payload, err := json.Marshal(map[string]any{
			"v": 1,
			"notice": map[string]string{
				"ID": notice.ID, "Title": notice.Title, "Summary": notice.Summary,
				"Link": notice.Link, "Date": notice.Published.Display(),
			},
		})
		if err != nil {
			t.Fatalf("marshal legacy payload: %v", err)
		}
		legacy = append(legacy, rfs.ExtractedItem{GUID: notice.ID, Link: notice.Link, Title: notice.Title, Description: string(payload)})
	}
	if err := store.SaveChanges(ctx, source.ID, rfs.ChangeState{Items: legacy, Version: 1, Revision: 7, Initialized: true}, nil, rfs.FetchCache{ExtractVersion: 1}); err != nil {
		t.Fatalf("seed legacy baseline: %v", err)
	}

	if items := pollIntoStore(t, store, source, routes); len(items) != 0 {
		t.Fatalf("the upgrade announced %#v, want a silent rebaseline", items)
	}
	state, err := store.LoadChangeState(ctx, source.ID)
	if err != nil {
		t.Fatalf("load change state: %v", err)
	}
	if state.Version != notices.ExtractVersion {
		t.Fatalf("baseline version = %d, want %d", state.Version, notices.ExtractVersion)
	}
	if state.Revision != 7 {
		t.Fatalf("revision = %d, want the unchanged 7", state.Revision)
	}
	if len(state.Items) != 10 {
		t.Fatalf("rebaselined %d notices, want all 10 the page lists", len(state.Items))
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
