package seadex_test

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ppowo/rfs/internal/rfs"
	"github.com/ppowo/rfs/internal/sources/seadex"
)

const before = `{"page":1,"perPage":100,"totalPages":1,"totalItems":1,"items":[{"id":"kingsgame","alID":99698,"updated":"2026-09-08 18:00:00.000Z","trs":["oldbest","oldalt"],"notes":"NOGRP is FRA BD Remux+CR and PGS, missing fonts and signs track is PGS only\nAlmighty is USA BD Encode+PGS\njsum would be a better alt","expand":{"trs":[{"id":"oldbest","releaseGroup":"NOGRP","isBest":true,"tags":[]},{"id":"oldalt","releaseGroup":"Almighty","isBest":false,"tags":[]}]}}]}`
const after = `{"page":1,"perPage":100,"totalPages":1,"totalItems":1,"items":[{"id":"kingsgame","alID":99698,"updated":"2026-09-08 19:18:03.639Z","trs":["newbest","newalt"],"notes":"Headpatter is FRA BD Remux+Restyled CR, Stock CR, and PGS\nHeadpatter is also jsum with the same subs","expand":{"trs":[{"id":"newbest","releaseGroup":"Headpatter","isBest":true,"tags":[]},{"id":"newalt","releaseGroup":"Headpatter","isBest":false,"tags":[]}]}}]}`

type feedItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
	Description string `xml:"description"`
}

func feed(t *testing.T, store *rfs.SQLiteStore, source rfs.Source) []feedItem {
	t.Helper()
	w := httptest.NewRecorder()
	rfs.NewHTTPHandler(store, []rfs.Source{source}, rfs.BuildInfo{}).ServeHTTP(w, httptest.NewRequest("GET", "/feeds/seadex.xml", nil))
	if w.Code != 200 {
		t.Fatalf("feed status %d: %s", w.Code, w.Body.String())
	}
	var doc struct {
		Items []feedItem `xml:"channel>item"`
	}
	if err := xml.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Items
}

func TestPaginationFailureDoesNotPublishOrConsumeChanges(t *testing.T) {
	second := strings.ReplaceAll(before, "99698", "2")
	failed := false
	failureStatus := 500
	page := func(body string, number int) string {
		body = strings.Replace(body, `"page":1`, fmt.Sprintf(`"page":%d`, number), 1)
		body = strings.Replace(body, `"totalPages":1`, `"totalPages":2`, 1)
		return strings.Replace(body, `"totalItems":1`, `"totalItems":2`, 1)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			if failed {
				w.Header().Set("Retry-After", "7200")
				http.Error(w, "broken", failureStatus)
				return
			}
			w.Write([]byte(page(second, 2)))
			return
		}
		w.Write([]byte(page(before, 1)))
	}))
	defer server.Close()
	store, err := rfs.OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	source := rfs.Source{ID: "seadex", URL: server.URL, Flow: seadex.Flow{}}
	poller := rfs.Poller{Fetcher: rfs.NewHTTPFetcher(server.Client()), Store: store}
	if _, err := poller.Poll(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	second = strings.ReplaceAll(after, "99698", "2")
	failed = true
	if _, err := poller.Poll(context.Background(), source); err == nil {
		t.Fatal("partial fetch must fail")
	}
	if items := feed(t, store, source); len(items) != 0 {
		t.Fatalf("partial fetch published changes: %+v", items)
	}
	failureStatus = 429
	result, err := poller.Poll(context.Background(), source)
	if err != nil || result.Status != rfs.PollThrottled || result.RetryAfter != 2*time.Hour {
		t.Fatalf("page throttle not propagated: %+v, %v", result, err)
	}
	failed = false
	if _, err := poller.Poll(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	items := feed(t, store, source)
	if len(items) != 1 || items[0].Link != "https://releases.moe/2/" || !strings.Contains(items[0].Description, "- NOGRP") {
		t.Fatalf("lost page-two change: %+v", items)
	}
}

func TestRestartRetainsHistoryAndPublishesRevertsAndRemovals(t *testing.T) {
	body := before
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "rfs.sqlite")
	source := rfs.Source{ID: "seadex", URL: server.URL, Flow: seadex.Flow{}}
	poll := func(bodyValue string) []feedItem {
		t.Helper()
		body = bodyValue
		store, err := rfs.OpenSQLiteStore(path)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		p := rfs.Poller{Fetcher: rfs.NewHTTPFetcher(server.Client()), Store: store}
		if _, err := p.Poll(context.Background(), source); err != nil {
			t.Fatal(err)
		}
		return feed(t, store, source)
	}
	if got := poll(before); len(got) != 0 {
		t.Fatal("baseline must be silent")
	}
	if got := poll(after); len(got) != 1 {
		t.Fatal("missing initial change")
	}
	if got := poll(after); len(got) != 1 {
		t.Fatal("restart duplicated change")
	}
	if got := poll(before); len(got) != 2 {
		t.Fatal("revert must publish a new change")
	}
	got := poll(after)
	if len(got) != 3 {
		t.Fatalf("second transition to same state lost: %+v", got)
	}
	seen := map[string]bool{}
	for _, item := range got {
		if seen[item.GUID] {
			t.Fatal("reused GUID")
		}
		seen[item.GUID] = true
	}
	empty := `{"page":1,"perPage":100,"totalPages":0,"totalItems":0,"items":[]}`
	got = poll(empty)
	if len(got) != 4 || !strings.Contains(got[0].Description, "- Headpatter") {
		t.Fatalf("removal missing: %+v", got)
	}
	if strings.Contains(got[0].Description, "Unmuxed Best") {
		t.Fatal("empty unchanged section appeared in removal")
	}
	if got = poll(after); len(got) != 5 {
		t.Fatal("re-addition missing")
	}
}

func TestUnmuxedBestAndTagsPublishWithoutGroupChanges(t *testing.T) {
	body := before
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
	defer server.Close()
	store, err := rfs.OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	source := rfs.Source{ID: "seadex", URL: server.URL, Flow: seadex.Flow{}}
	p := rfs.Poller{Fetcher: rfs.NewHTTPFetcher(server.Client()), Store: store}
	if _, err := p.Poll(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	body = strings.Replace(before, `"notes":`, `"theoreticalBest":"JPN BD+CR","incomplete":true,"comparison":"https://slow.pics/c/new","notes":`, 1)
	body = strings.Replace(body, `"tags":[]`, `"tags":["Deband Recommended"],"dualAudio":true`, 1)
	if _, err := p.Poll(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	got := feed(t, store, source)
	if len(got) != 1 {
		t.Fatalf("want one update, got %+v", got)
	}
	for _, text := range []string{"Unmuxed Best", "+ JPN BD+CR", "Tags", "+ NOGRP: Deband Recommended", "Incomplete", "Dual Audio", "Comparisons"} {
		if !strings.Contains(got[0].Description, text) {
			t.Errorf("missing %q in %s", text, got[0].Description)
		}
	}
	if strings.Contains(got[0].Description, "Notes") {
		t.Fatal("unchanged notes should not appear")
	}
}

func TestUpdatesIncludeAnimeTitleCoverAndSafeHTML(t *testing.T) {
	body := before
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metadata" {
			calls++
			var request struct {
				Query string `json:"query"`
			}
			if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&request) != nil || !strings.Contains(request.Query, "99698") {
				t.Error("expected AniList JSON query for changed anime")
			}
			w.Write([]byte(`{"data":{"Page":{"media":[{"id":99698,"title":{"english":"King's Game","romaji":"Ousama Game"},"coverImage":{"large":"https://example.org/cover.jpg"}}]}}}`))
			return
		}
		w.Write([]byte(body))
	}))
	defer server.Close()
	store, err := rfs.OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	source := rfs.Source{ID: "seadex", URL: server.URL, Meta: rfs.SourceMeta{ItemDescriptionsHTML: true}, Flow: seadex.Flow{MetadataURL: server.URL + "/metadata"}}
	p := rfs.Poller{Fetcher: rfs.NewHTTPFetcher(server.Client()), Store: store}
	if _, err := p.Poll(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("baseline should not fetch metadata")
	}
	body = strings.Replace(after, "Headpatter is also jsum with the same subs", "<script>alert(1)</script>", 1)
	if _, err := p.Poll(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	got := feed(t, store, source)
	if len(got) != 1 || got[0].Title != "King's Game" {
		t.Fatalf("missing anime title: %+v", got)
	}
	for _, text := range []string{`<img src="https://example.org/cover.jpg"`, "<h3>Best</h3>", "&lt;script&gt;alert(1)&lt;/script&gt;"} {
		if !strings.Contains(got[0].Description, text) {
			t.Errorf("missing %q in %s", text, got[0].Description)
		}
	}
	if strings.Contains(got[0].Description, "<script>") {
		t.Fatal("upstream HTML not escaped")
	}
	if _, err := p.Poll(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	rfs.NewHTTPHandler(store, []rfs.Source{source}, rfs.BuildInfo{}).ServeHTTP(w, httptest.NewRequest("GET", "/feeds/seadex.html", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "<h3>Best</h3>") || strings.Contains(w.Body.String(), "<script>") {
		t.Fatalf("unsafe or escaped HTML view: %s", w.Body.String())
	}
	if calls != 1 {
		t.Fatalf("unchanged poll requested metadata again: %d calls", calls)
	}
}

func TestMalformedResponsesCannotEraseRecommendations(t *testing.T) {
	for name, broken := range map[string]string{
		"missing recommendation rank": strings.Replace(before, `"isBest":`, `"missingRank":`, 1),
		"missing expansion":           strings.Replace(before, `"expand":`, `"missing":`, 1),
		"missing items":               `{"page":1,"totalPages":0,"totalItems":0}`,
		"missing identity":            strings.Replace(before, `"alID":99698,`, "", 1),
		"missing pagination totals":   `{"page":1,"items":[]}`,
		"duplicate entities":          strings.Replace(before, `"totalItems":1`, `"totalItems":2`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			body := before
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
			defer server.Close()
			store, err := rfs.OpenInMemorySQLiteStore()
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			source := rfs.Source{ID: "seadex", URL: server.URL, Flow: seadex.Flow{}}
			p := rfs.Poller{Fetcher: rfs.NewHTTPFetcher(server.Client()), Store: store}
			if _, err := p.Poll(context.Background(), source); err != nil {
				t.Fatal(err)
			}
			body = broken
			if _, err := p.Poll(context.Background(), source); err == nil {
				t.Fatal("malformed response accepted")
			}
			if got := feed(t, store, source); len(got) != 0 {
				t.Fatalf("false update: %+v", got)
			}
			body = after
			if _, err := p.Poll(context.Background(), source); err != nil {
				t.Fatal(err)
			}
			if got := feed(t, store, source); len(got) != 1 || !strings.Contains(got[0].Description, "- NOGRP") {
				t.Fatalf("baseline lost: %+v", got)
			}
		})
	}
}

func TestNoteReorderingIsAnUpdate(t *testing.T) {
	body := strings.Replace(before, `"notes":`, `"unused":`, 1)
	body = strings.Replace(body, `"unused":`, `"notes":"first\nsecond\nfirst","unused":`, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
	defer server.Close()
	store, err := rfs.OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	source := rfs.Source{ID: "seadex", URL: server.URL, Flow: seadex.Flow{}}
	p := rfs.Poller{Fetcher: rfs.NewHTTPFetcher(server.Client()), Store: store}
	if _, err := p.Poll(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	body = strings.Replace(body, `first\nsecond\nfirst`, `first\nfirst\nsecond`, 1)
	if _, err := p.Poll(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	if got := feed(t, store, source); len(got) != 1 || !strings.Contains(got[0].Description, "Notes") {
		t.Fatalf("reordered notes lost: %+v", got)
	}
}

func TestReleaseReplacementWithinSameGroupPublishes(t *testing.T) {
	body := before
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
	defer server.Close()
	store, err := rfs.OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	source := rfs.Source{ID: "seadex", URL: server.URL, Flow: seadex.Flow{}}
	p := rfs.Poller{Fetcher: rfs.NewHTTPFetcher(server.Client()), Store: store}
	if _, err := p.Poll(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	body = strings.ReplaceAll(before, "oldbest", "replacement")
	if _, err := p.Poll(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	got := feed(t, store, source)
	if len(got) != 1 || !strings.Contains(got[0].Description, "Releases") {
		t.Fatalf("same-group replacement lost: %+v", got)
	}
}

func TestMetadataThrottlePreservesPendingChangeAndRetryAfter(t *testing.T) {
	body := before
	throttled := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metadata" {
			if throttled {
				w.Header().Set("Retry-After", "7200")
				w.WriteHeader(429)
				return
			}
			w.Write([]byte(`{"data":{"Page":{"media":[{"id":99698,"title":{"english":"King's Game"}}]}}}`))
			return
		}
		w.Write([]byte(body))
	}))
	defer server.Close()
	store, err := rfs.OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	source := rfs.Source{ID: "seadex", URL: server.URL, Flow: seadex.Flow{MetadataURL: server.URL + "/metadata"}}
	p := rfs.Poller{Fetcher: rfs.NewHTTPFetcher(server.Client()), Store: store}
	if _, err := p.Poll(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	body = after
	result, err := p.Poll(context.Background(), source)
	if err != nil || result.Status != rfs.PollThrottled || result.RetryAfter != 2*time.Hour {
		t.Fatalf("throttle not propagated: %+v, %v", result, err)
	}
	if got := feed(t, store, source); len(got) != 0 {
		t.Fatal("failed enrichment published update")
	}
	throttled = false
	if _, err := p.Poll(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	got := feed(t, store, source)
	if len(got) != 1 || got[0].Title != "King's Game" || !strings.Contains(got[0].Description, "- NOGRP") {
		t.Fatalf("pending change lost: %+v", got)
	}
}

func TestUnavailableCoverDoesNotBlockTextUpdate(t *testing.T) {
	body := before
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metadata" {
			w.Write([]byte(`{"errors":[{"message":"image unavailable","path":["Page","media",0,"coverImage"]}],"data":{"Page":{"media":[{"id":99698,"title":{"english":"King's Game"},"coverImage":null}]}}}`))
			return
		}
		w.Write([]byte(body))
	}))
	defer server.Close()
	store, err := rfs.OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	source := rfs.Source{ID: "seadex", URL: server.URL, Flow: seadex.Flow{MetadataURL: server.URL + "/metadata"}}
	p := rfs.Poller{Fetcher: rfs.NewHTTPFetcher(server.Client()), Store: store}
	if _, err := p.Poll(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	body = after
	if _, err := p.Poll(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	got := feed(t, store, source)
	if len(got) != 1 || got[0].Title != "King's Game" || strings.Contains(got[0].Description, "<img") || !strings.Contains(got[0].Description, "- NOGRP") {
		t.Fatalf("missing text-only update: %+v", got)
	}
}

func TestKingsGameRecommendationDiffAppearsAfterSilentBaseline(t *testing.T) {
	body := before
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
	defer server.Close()
	store, err := rfs.OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	source := rfs.Source{ID: "seadex", URL: server.URL, Flow: seadex.Flow{}}
	poller := rfs.Poller{Fetcher: rfs.NewHTTPFetcher(server.Client()), Store: store}
	poll := func() {
		t.Helper()
		if _, err := poller.Poll(context.Background(), source); err != nil {
			t.Fatal(err)
		}
	}
	poll()
	if got := feed(t, store, source); len(got) != 0 {
		t.Fatalf("baseline published %d items", len(got))
	}
	body = after
	poll()
	items := feed(t, store, source)
	if len(items) != 1 {
		t.Fatalf("want one update, got %d", len(items))
	}
	for _, text := range []string{"Best", "Alt", "Notes", "- NOGRP", "- Almighty", "+ Headpatter", "- jsum would be a better alt", "+ Headpatter is also jsum with the same subs"} {
		if !strings.Contains(items[0].Description, text) {
			t.Errorf("missing %q in %s", text, items[0].Description)
		}
	}
	if items[0].Link != "https://releases.moe/99698/" {
		t.Errorf("link = %s", items[0].Link)
	}
	poll()
	repeated := feed(t, store, source)
	if len(repeated) != 1 || repeated[0].GUID != items[0].GUID {
		t.Fatalf("unchanged poll duplicated update: %+v", repeated)
	}
}
