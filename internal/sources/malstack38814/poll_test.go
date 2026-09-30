package malstack38814_test

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kitevi/rfs/internal/rfs"
	"github.com/kitevi/rfs/internal/sources/malstack38814"
)

type fixedClock struct {
	at time.Time
}

func (c *fixedClock) Now() time.Time { return c.at }

type feedItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
	Description string `xml:"description"`
	PubDate     string `xml:"pubDate"`
}

func testSource(url string) rfs.Source {
	return rfs.Source{
		ID:  "mal-stack-38814",
		URL: url,
		Meta: rfs.SourceMeta{
			Title:                "MyAnimeList: The Next Sakuga Shows",
			Description:          "Observed additions, removals, and curator note edits to the Interest Stack.",
			Link:                 malstack38814.HumanURL,
			ItemDescriptionsHTML: true,
		},
		Flow: malstack38814.Flow{},
	}
}

func readFeed(t *testing.T, store *rfs.SQLiteStore, source rfs.Source) []feedItem {
	t.Helper()
	w := httptest.NewRecorder()
	rfs.NewHTTPHandler(store, []rfs.Source{source}, rfs.BuildInfo{}).ServeHTTP(w, httptest.NewRequest("GET", "/feeds/mal-stack-38814.xml", nil))
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

func readFeedHTML(t *testing.T, store *rfs.SQLiteStore, source rfs.Source) string {
	t.Helper()
	w := httptest.NewRecorder()
	rfs.NewHTTPHandler(store, []rfs.Source{source}, rfs.BuildInfo{}).ServeHTTP(w, httptest.NewRequest("GET", "/feeds/mal-stack-38814.html", nil))
	if w.Code != 200 {
		t.Fatalf("feed HTML status %d: %s", w.Code, w.Body.String())
	}
	return w.Body.String()
}

func TestPollPublishesChangesThroughFeed(t *testing.T) {
	body := stackHTML(2,
		testEntry{id: "59878", title: "Alpha", notes: "Old note<br>Tail"},
		testEntry{id: "60000", title: "Beta", notes: "Beta note"},
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
	defer server.Close()
	store, err := rfs.OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	source := testSource(server.URL)
	t0 := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	clock := &fixedClock{at: t0}
	poller := rfs.Poller{Fetcher: rfs.NewHTTPFetcher(server.Client()), Store: store, Clock: clock}

	if _, err := poller.Poll(context.Background(), source); err != nil {
		t.Fatalf("baseline poll: %v", err)
	}
	if items := readFeed(t, store, source); len(items) != 0 {
		t.Fatalf("first complete observation must stay silent, got %+v", items)
	}

	body = stackHTML(2,
		testEntry{id: "59878", title: "Alpha", notes: "New note<br>Tail"},
		testEntry{id: "60001", title: "Gamma", notes: ""},
	)
	clock.at = t0.Add(time.Hour)
	if _, err := poller.Poll(context.Background(), source); err != nil {
		t.Fatalf("changed poll: %v", err)
	}
	items := readFeed(t, store, source)
	if len(items) != 3 {
		t.Fatalf("expected one event per affected anime, got %d: %+v", len(items), items)
	}
	wantTitles := []string{"Notes updated: Alpha", "Removed: Beta", "Added: Gamma"}
	wantGUIDs := []string{"mal-stack-38814:1:59878", "mal-stack-38814:1:60000", "mal-stack-38814:1:60001"}
	for i := range items {
		if items[i].Title != wantTitles[i] || items[i].GUID != wantGUIDs[i] {
			t.Fatalf("item %d = %q (%s), want %q (%s)", i, items[i].Title, items[i].GUID, wantTitles[i], wantGUIDs[i])
		}
		if want := t0.Add(time.Hour).Format(time.RFC1123Z); items[i].PubDate != want {
			t.Fatalf("item %d pubDate = %q, want observation time %q", i, items[i].PubDate, want)
		}
		if items[i].Link != "https://myanimelist.net/anime/"+strings.TrimPrefix(wantGUIDs[i], "mal-stack-38814:1:") {
			t.Fatalf("item %d link = %q", i, items[i].Link)
		}
	}

	clock.at = t0.Add(2 * time.Hour)
	if _, err := poller.Poll(context.Background(), source); err != nil {
		t.Fatalf("unchanged poll: %v", err)
	}
	if again := readFeed(t, store, source); len(again) != 3 {
		t.Fatalf("unchanged observation duplicated events: %d", len(again))
	}
}

func TestPollPreservesBaselineOnIncompleteAndFailedPages(t *testing.T) {
	body := stackHTML(1, testEntry{id: "59878", title: "Alpha", notes: "Old note"})
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			if status == http.StatusTooManyRequests {
				w.Header().Set("Retry-After", "3600")
			}
			http.Error(w, "upstream down", status)
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
	source := testSource(server.URL)
	poller := rfs.Poller{Fetcher: rfs.NewHTTPFetcher(server.Client()), Store: store, Clock: &fixedClock{at: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}}

	if _, err := poller.Poll(context.Background(), source); err != nil {
		t.Fatalf("baseline poll: %v", err)
	}
	body = stackHTML(1, testEntry{id: "59878", title: "Alpha", notes: "New note"})
	changed := pollAndExpectChange(t, poller, source, store, "Notes updated: Alpha")

	// Dropping the canonical URL metadata makes the page unrecognizable even
	// though the remaining HTML still parses as a full collection.
	body = strings.Replace(body, `<meta property="og:url" content="https://myanimelist.net/stacks/38814">`, "", 1)
	if _, err := poller.Poll(context.Background(), source); err == nil {
		t.Fatal("unrecognized page must fail the poll")
	}
	if items := readFeed(t, store, source); len(items) != len(changed) {
		t.Fatalf("failed page changed the feed: %d items", len(items))
	}

	status = http.StatusInternalServerError
	if _, err := poller.Poll(context.Background(), source); err == nil {
		t.Fatal("HTTP failure must fail the poll")
	}
	status = http.StatusTooManyRequests
	result, err := poller.Poll(context.Background(), source)
	if err != nil || result.Status != rfs.PollThrottled || result.RetryAfter != time.Hour {
		t.Fatalf("throttle not propagated: %+v, %v", result, err)
	}
	status = http.StatusOK
	body = stackHTML(1, testEntry{id: "60001", title: "Gamma", notes: "Gamma note"})
	if _, err := poller.Poll(context.Background(), source); err != nil {
		t.Fatalf("recovery poll: %v", err)
	}
	items := readFeed(t, store, source)
	if len(items) != len(changed)+2 {
		t.Fatalf("recovery must publish the pending removal and addition: %+v", items)
	}
}

func pollAndExpectChange(t *testing.T, poller rfs.Poller, source rfs.Source, store *rfs.SQLiteStore, title string) []feedItem {
	t.Helper()
	if _, err := poller.Poll(context.Background(), source); err != nil {
		t.Fatalf("poll: %v", err)
	}
	items := readFeed(t, store, source)
	if len(items) == 0 || items[len(items)-1].Title != title {
		t.Fatalf("expected %q in feed, got %+v", title, items)
	}
	return items
}

func TestPollRestartRetainsHistoryAndDistinctRevisionGUIDs(t *testing.T) {
	body := stackHTML(1, testEntry{id: "59878", title: "Alpha", notes: "Alpha note"})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "rfs.sqlite")
	store, err := rfs.OpenSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	source := testSource(server.URL)
	poller := rfs.Poller{Fetcher: rfs.NewHTTPFetcher(server.Client()), Store: store, Clock: &fixedClock{at: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}}
	if _, err := poller.Poll(context.Background(), source); err != nil {
		t.Fatalf("baseline poll: %v", err)
	}
	body = stackHTML(2,
		testEntry{id: "59878", title: "Alpha", notes: "Alpha note"},
		testEntry{id: "60001", title: "Gamma", notes: "Gamma note"},
	)
	first := pollAndExpectChange(t, poller, source, store, "Added: Gamma")
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = rfs.OpenSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	poller.Store = store
	body = stackHTML(1, testEntry{id: "59878", title: "Alpha", notes: "Alpha note"})
	removed := pollAndExpectChange(t, poller, source, store, "Removed: Gamma")
	body = stackHTML(2,
		testEntry{id: "59878", title: "Alpha", notes: "Alpha note"},
		testEntry{id: "60001", title: "Gamma", notes: "Gamma note"},
	)
	readded := pollAndExpectChange(t, poller, source, store, "Added: Gamma")

	if len(first) != 1 {
		t.Fatalf("restart lost history: %+v", first)
	}
	if len(removed) != 2 || len(readded) != 3 {
		t.Fatalf("history lost across restart: removed=%d readded=%d", len(removed), len(readded))
	}
	addGUID := first[0].GUID
	reAddGUID := readded[2].GUID
	if addGUID == reAddGUID {
		t.Fatalf("re-added anime reused GUID %q", addGUID)
	}
	if !strings.HasPrefix(addGUID, "mal-stack-38814:1:60001") || !strings.HasPrefix(reAddGUID, "mal-stack-38814:3:60001") {
		t.Fatalf("unexpected revision GUIDs: add=%q re-add=%q", addGUID, reAddGUID)
	}
}

func TestPollRendersHostileUpstreamTextInertly(t *testing.T) {
	body := stackHTML(1, testEntry{id: "59878", title: "Alpha", notes: "Plain note"})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) }))
	defer server.Close()
	store, err := rfs.OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	source := testSource(server.URL)
	poller := rfs.Poller{Fetcher: rfs.NewHTTPFetcher(server.Client()), Store: store, Clock: &fixedClock{at: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}}
	if _, err := poller.Poll(context.Background(), source); err != nil {
		t.Fatalf("baseline poll: %v", err)
	}
	hostile := `&lt;script&gt;alert(1)&lt;/script&gt;<img src=x onerror=alert(2)><a href="javascript:evil()">click</a>`
	body = stackHTML(1, testEntry{id: "59878", title: "Alpha", notes: hostile})
	if _, err := poller.Poll(context.Background(), source); err != nil {
		t.Fatalf("hostile poll: %v", err)
	}
	htmlBody := readFeedHTML(t, store, source)
	if strings.Contains(htmlBody, "<script>alert(1)</script>") {
		t.Fatalf("hostile script markup passed into the HTML view: %q", htmlBody)
	}
	if strings.Contains(htmlBody, "onerror=alert(2)") {
		t.Fatal("hostile attribute passed into the HTML view")
	}
	if strings.Contains(htmlBody, `href="javascript:evil()"`) {
		t.Fatal("javascript link passed into the HTML view")
	}
	if !strings.Contains(htmlBody, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatalf("escaped hostile text should remain readable: %q", htmlBody)
	}
	items := readFeed(t, store, source)
	if len(items) != 1 || !strings.Contains(items[0].Description, "&lt;script&gt;") {
		t.Fatalf("RSS must escape hostile text: %+v", items)
	}
}
