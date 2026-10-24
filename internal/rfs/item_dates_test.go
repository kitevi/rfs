package rfs

import (
	"context"
	"errors"
	"testing"
	"time"
)

// datedStub lists items without dates and reads each item's date from its
// page, whose body is the date in RFC 3339.
type datedStub struct{ items []ExtractedItem }

func (f datedStub) Version() int { return 1 }

func (f datedStub) Extract(Page) ([]ExtractedItem, error) { return f.items, nil }

func (datedStub) ItemDateURL(item ExtractedItem) (string, error) { return item.Link, nil }

func (datedStub) ItemDate(page Page, _ ExtractedItem) (time.Time, error) {
	return time.Parse(time.RFC3339, string(page))
}

// datedFetcher serves the collection with validators and records every item
// page it is asked for.
type datedFetcher struct {
	urlFetcher
	requests []string
}

func (f *datedFetcher) Fetch(ctx context.Context, url string, cache FetchCache) (FetchResult, error) {
	f.requests = append(f.requests, url)
	result, err := f.urlFetcher.Fetch(ctx, url, cache)
	if err == nil && url == "https://example.com/index" {
		result.Cache = FetchCache{ETag: `"v1"`}
	}
	return result, err
}

func datedItem(slug string) ExtractedItem {
	return ExtractedItem{GUID: "essay:" + slug, Title: slug, Link: "https://example.com/" + slug}
}

func TestPollerDatesItemsFromTheirPagesOnce(t *testing.T) {
	store, err := OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Date(2026, 10, 22, 15, 0, 0, 0, time.UTC)
	fetcher := &datedFetcher{urlFetcher: urlFetcher{pages: map[string]Page{
		"https://example.com/index":   Page("<index>"),
		"https://example.com/nervous": Page("2026-08-28T00:00:00Z"),
	}}}
	recorder := &secondaryRecorder{}
	source := Source{ID: "essays", URL: "https://example.com/index", Flow: datedStub{items: []ExtractedItem{datedItem("nervous")}}}
	poller := Poller{Fetcher: fetcher, Store: store, Clock: fixedClock{now: now}, SecondaryFetch: recorder.guard}

	for range 2 {
		if _, err := poller.Poll(t.Context(), source); err != nil {
			t.Fatalf("poll: %v", err)
		}
	}
	items, err := store.LoadSnapshot(t.Context(), "essays")
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC); len(items) != 1 || !items[0].PubDate.Equal(want) {
		t.Fatalf("snapshot = %#v, want the page's date %v", items, want)
	}
	pageFetches := 0
	for _, url := range fetcher.requests {
		if url == "https://example.com/nervous" {
			pageFetches++
		}
	}
	if pageFetches != 1 {
		t.Fatalf("item page fetched %d times, want once: %v", pageFetches, fetcher.requests)
	}
	if len(recorder.urls) != 1 || recorder.urls[0] != "https://example.com/nervous" {
		t.Fatalf("guarded follow-up requests = %v, want the item page", recorder.urls)
	}
	cache, err := store.LoadFetchCache(t.Context(), "essays")
	if err != nil {
		t.Fatal(err)
	}
	if cache.ETag != `"v1"` {
		t.Fatalf("fetch cache = %#v, want the collection validators", cache)
	}
}

func TestPollerFallsBackToFirstSeenAndRetriesAnUnreadablePage(t *testing.T) {
	store, err := OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	now := time.Date(2026, 10, 22, 15, 0, 0, 0, time.UTC)
	fetcher := &datedFetcher{urlFetcher: urlFetcher{
		pages: map[string]Page{
			"https://example.com/index":  Page("<index>"),
			"https://example.com/advice": Page("2026-09-05T00:00:00Z"),
		},
		err: map[string]error{"https://example.com/nervous": errors.New("connection reset")},
	}}
	source := Source{ID: "essays", URL: "https://example.com/index", Flow: datedStub{items: []ExtractedItem{datedItem("nervous"), datedItem("advice")}}}
	poller := Poller{Fetcher: fetcher, Store: store, Clock: fixedClock{now: now}}

	if _, err := poller.Poll(t.Context(), source); err != nil {
		t.Fatalf("poll: %v", err)
	}
	items, err := store.LoadSnapshot(t.Context(), "essays")
	if err != nil {
		t.Fatal(err)
	}
	byGUID := map[string]Item{}
	for _, item := range items {
		byGUID[item.GUID] = item
	}
	if !byGUID["essay:nervous"].PubDate.Equal(now) {
		t.Fatalf("unreadable item = %#v, want the first-seen date", byGUID["essay:nervous"])
	}
	if want := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC); !byGUID["essay:advice"].PubDate.Equal(want) {
		t.Fatalf("readable item = %#v, want %v", byGUID["essay:advice"], want)
	}
	cache, err := store.LoadFetchCache(t.Context(), "essays")
	if err != nil {
		t.Fatal(err)
	}
	if cache.ETag != "" || cache.LastModified != "" {
		t.Fatalf("fetch cache = %#v, want no validators while a date is pending", cache)
	}

	delete(fetcher.err, "https://example.com/nervous")
	fetcher.pages["https://example.com/nervous"] = Page("2026-08-28T00:00:00Z")
	if _, err := poller.Poll(t.Context(), source); err != nil {
		t.Fatalf("retry poll: %v", err)
	}
	items, err = store.LoadSnapshot(t.Context(), "essays")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.GUID == "essay:nervous" && !item.PubDate.Equal(time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC)) {
			t.Fatalf("retried item = %#v, want the page's date", item)
		}
	}
}

func TestPollerStopsDatingWhenAnItemPageThrottles(t *testing.T) {
	store, err := OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	fetcher := throttlingItemFetcher{index: "https://example.com/index"}
	source := Source{ID: "essays", URL: "https://example.com/index", Flow: datedStub{items: []ExtractedItem{datedItem("nervous")}}}
	poller := Poller{Fetcher: fetcher, Store: store}

	result, err := poller.Poll(t.Context(), source)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if result.Status != PollThrottled || result.RetryAfter != time.Minute {
		t.Fatalf("result = %#v, want throttled for a minute", result)
	}
	items, err := store.LoadSnapshot(t.Context(), "essays")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("snapshot = %#v, want nothing committed", items)
	}
}

type throttlingItemFetcher struct{ index string }

func (f throttlingItemFetcher) Fetch(_ context.Context, url string, _ FetchCache) (FetchResult, error) {
	if url == f.index {
		return FetchResult{Status: FetchModified, Page: Page("<index>")}, nil
	}
	return FetchResult{Status: FetchThrottled, RetryAfter: time.Minute}, nil
}
