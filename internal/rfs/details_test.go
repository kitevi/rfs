package rfs

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// detailFetcher serves a fixed URL set and records what the engine requested,
// including the conditional-fetch cache it passed for each request.
type detailFetcher struct {
	pages  map[string]FetchResult
	urls   []string
	caches []FetchCache
}

func (f *detailFetcher) Fetch(_ context.Context, url string, cache FetchCache) (FetchResult, error) {
	f.urls = append(f.urls, url)
	f.caches = append(f.caches, cache)
	result, ok := f.pages[url]
	if !ok {
		return FetchResult{}, fmt.Errorf("unexpected fetch %s", url)
	}
	return result, nil
}

// detailFlow extracts a collection only after rfs fetched its detail pages.
type detailFlow struct {
	urls          []string
	urlsErr       error
	result        []ExtractedItem
	details       []Page
	collection    Page
	changes       func(previous, current []ExtractedItem) ([]ExtractedItem, error)
	detailCalls   int
	plainCalls    int
	version       int
	ExtractCalled bool
}

func (f *detailFlow) Version() int { return f.version }

func (f *detailFlow) Extract(Page) ([]ExtractedItem, error) {
	f.plainCalls++
	return nil, errors.New("Extract must not run for a DetailFlow")
}

func (f *detailFlow) DetailURLs(Page) ([]string, error) {
	if f.urlsErr != nil {
		return nil, f.urlsErr
	}
	return f.urls, nil
}

func (f *detailFlow) ExtractDetails(collection Page, details []Page) ([]ExtractedItem, error) {
	f.detailCalls++
	f.collection = collection
	f.details = details
	return f.result, nil
}

func (f *detailFlow) Changes(previous, current []ExtractedItem) ([]ExtractedItem, error) {
	if f.changes == nil {
		return nil, nil
	}
	return f.changes(previous, current)
}

func newDetailStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestPollerFetchesDetailPagesBeforeComparison(t *testing.T) {
	ctx := context.Background()
	store := newDetailStore(t)
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	fetcher := &detailFetcher{pages: map[string]FetchResult{
		"https://example.com/scioperi/":   {Status: FetchModified, Page: Page("<index>")},
		"https://example.com/scioperi/1/": {Status: FetchModified, Page: Page("<notice 1>")},
		"https://example.com/scioperi/2/": {Status: FetchModified, Page: Page("<notice 2>")},
	}}
	flow := &detailFlow{
		urls:   []string{"/scioperi/1/", "https://example.com/scioperi/2/"},
		result: []ExtractedItem{{GUID: "https://example.com/scioperi/1/", Title: "N1"}},
	}
	poller := Poller{Fetcher: fetcher, Store: store, Clock: fixedClock{now: now}}

	result, err := poller.Poll(ctx, Source{ID: "tpl", URL: "https://example.com/scioperi/", Flow: flow})
	if err != nil {
		t.Fatalf("Poll returned error: %v", err)
	}
	if result.Status != PollUpdated {
		t.Fatalf("status = %v, want updated", result.Status)
	}
	if flow.plainCalls != 0 {
		t.Fatalf("Extract ran %d times for a DetailFlow", flow.plainCalls)
	}
	if flow.detailCalls != 1 {
		t.Fatalf("ExtractDetails ran %d times, want 1", flow.detailCalls)
	}
	wantURLs := []string{"https://example.com/scioperi/", "https://example.com/scioperi/1/", "https://example.com/scioperi/2/"}
	if !reflect.DeepEqual(fetcher.urls, wantURLs) {
		t.Fatalf("fetched %v, want %v", fetcher.urls, wantURLs)
	}
	for i, cache := range fetcher.caches {
		if cache != (FetchCache{}) {
			t.Fatalf("fetch %d passed cache %#v, want unconditional fetch", i, cache)
		}
	}
	if string(flow.collection) != "<index>" {
		t.Fatalf("ExtractDetails collection = %q, want the collection page", flow.collection)
	}
	if len(flow.details) != 2 || string(flow.details[1]) != "<notice 2>" {
		t.Fatalf("ExtractDetails details = %q, want declared order", flow.details)
	}
	state, err := store.LoadChangeState(ctx, "tpl")
	if err != nil {
		t.Fatalf("load change state: %v", err)
	}
	if !state.Initialized || len(state.Items) != 1 || state.Items[0].GUID != "https://example.com/scioperi/1/" {
		t.Fatalf("baseline = %#v, want the extracted item", state)
	}
}

func TestPollerRejectsInvalidDetailURLsBeforeFetching(t *testing.T) {
	cases := []struct {
		name string
		url  string
	}{
		{"plain HTTP", "http://example.com/scioperi/1/"},
		{"other host", "https://evil.example.net/scioperi/1/"},
		{"host suffix", "https://example.com.evil.net/scioperi/1/"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			store := newDetailStore(t)
			fetcher := &detailFetcher{pages: map[string]FetchResult{
				"https://example.com/scioperi/": {Status: FetchModified, Page: Page("<index>")},
			}}
			flow := &detailFlow{urls: []string{tc.url}}
			poller := Poller{Fetcher: fetcher, Store: store, Clock: fixedClock{now: time.Now()}}

			_, err := poller.Poll(ctx, Source{ID: "tpl", URL: "https://example.com/scioperi/", Flow: flow})
			if err == nil {
				t.Fatalf("Poll accepted detail URL %q", tc.url)
			}
			if len(fetcher.urls) != 1 {
				t.Fatalf("fetcher requests = %v, want only the collection fetch", fetcher.urls)
			}
			if flow.detailCalls != 0 {
				t.Fatal("ExtractDetails ran despite an invalid detail URL")
			}
		})
	}
}

func TestPollerRejectsDuplicateAndExcessiveDetailURLs(t *testing.T) {
	cases := map[string][]string{
		"duplicate": {"/scioperi/1/", "/scioperi/1/"},
		"excessive": make([]string, 0, 101),
	}
	for i := 0; i < 101; i++ {
		cases["excessive"] = append(cases["excessive"], fmt.Sprintf("/scioperi/%d/", i))
	}
	for name, urls := range cases {
		t.Run(name, func(t *testing.T) {
			store := newDetailStore(t)
			fetcher := &detailFetcher{pages: map[string]FetchResult{
				"https://example.com/scioperi/": {Status: FetchModified, Page: Page("<index>")},
			}}
			flow := &detailFlow{urls: urls}
			poller := Poller{Fetcher: fetcher, Store: store, Clock: fixedClock{now: time.Now()}}

			_, err := poller.Poll(context.Background(), Source{ID: "tpl", URL: "https://example.com/scioperi/", Flow: flow})
			if err == nil {
				t.Fatalf("Poll accepted %s detail URLs", name)
			}
			if len(fetcher.urls) != 1 {
				t.Fatalf("fetcher requests = %v, want only the collection fetch", fetcher.urls)
			}
		})
	}
}

func TestPollerRefusesNotModifiedCollectionForDetailFlow(t *testing.T) {
	ctx := context.Background()
	store := newDetailStore(t)
	fetcher := &detailFetcher{pages: map[string]FetchResult{
		"https://example.com/scioperi/": {Status: FetchNotModified, Cache: FetchCache{ETag: `"v1"`}},
	}}
	flow := &detailFlow{urls: []string{"/scioperi/1/"}}
	poller := Poller{Fetcher: fetcher, Store: store, Clock: fixedClock{now: time.Now()}}

	_, err := poller.Poll(ctx, Source{ID: "tpl", URL: "https://example.com/scioperi/", Flow: flow})
	if err == nil {
		t.Fatal("a 304 cannot refresh the active detail pages")
	}
	if !strings.Contains(err.Error(), "304") {
		t.Fatalf("error = %v, want an explicit not-modified diagnosis", err)
	}
}

func TestPollerPropagatesDetailThrottle(t *testing.T) {
	ctx := context.Background()
	store := newDetailStore(t)
	fetcher := &detailFetcher{pages: map[string]FetchResult{
		"https://example.com/scioperi/":   {Status: FetchModified, Page: Page("<index>")},
		"https://example.com/scioperi/1/": {Status: FetchThrottled, RetryAfter: 45 * time.Second},
	}}
	flow := &detailFlow{urls: []string{"/scioperi/1/"}}
	poller := Poller{Fetcher: fetcher, Store: store, Clock: fixedClock{now: time.Now()}}

	result, err := poller.Poll(ctx, Source{ID: "tpl", URL: "https://example.com/scioperi/", Flow: flow})
	if err != nil {
		t.Fatalf("Poll returned error: %v", err)
	}
	if result.Status != PollThrottled || result.RetryAfter != 45*time.Second {
		t.Fatalf("result = %#v, want throttled with 45s", result)
	}
}

func TestPollerPreservesBaselineWhenDetailFetchFails(t *testing.T) {
	ctx := context.Background()
	store := newDetailStore(t)
	fetcher := &detailFetcher{pages: map[string]FetchResult{
		"https://example.com/scioperi/":   {Status: FetchModified, Page: Page("<index>")},
		"https://example.com/scioperi/1/": {Status: FetchModified, Page: Page("<notice 1>")},
	}}
	flow := &detailFlow{
		urls:   []string{"/scioperi/1/"},
		result: []ExtractedItem{{GUID: "n1", Title: "N1"}},
	}
	source := Source{ID: "tpl", URL: "https://example.com/scioperi/", Flow: flow}
	poller := Poller{Fetcher: fetcher, Store: store, Clock: fixedClock{now: time.Now()}}
	if _, err := poller.Poll(ctx, source); err != nil {
		t.Fatalf("first poll: %v", err)
	}

	// The next poll cannot assemble the observation: one detail page is gone.
	flow.result = []ExtractedItem{{GUID: "n2", Title: "N2"}}
	fetcher.pages = map[string]FetchResult{
		"https://example.com/scioperi/": {Status: FetchModified, Page: Page("<index>")},
	}
	if _, err := poller.Poll(ctx, source); err == nil {
		t.Fatal("second poll committed an incomplete detail collection")
	}
	state, err := store.LoadChangeState(ctx, "tpl")
	if err != nil {
		t.Fatalf("load change state: %v", err)
	}
	if len(state.Items) != 1 || state.Items[0].GUID != "n1" {
		t.Fatalf("baseline = %#v, want the preserved n1 item", state.Items)
	}
	items, err := store.LoadSnapshot(ctx, "tpl")
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("failed poll emitted %d items, want none", len(items))
	}
}

func TestPollerComparesRefreshedDetailsWithRevisionGUID(t *testing.T) {
	ctx := context.Background()
	store := newDetailStore(t)
	fetcher := &detailFetcher{pages: map[string]FetchResult{
		"https://example.com/scioperi/":   {Status: FetchModified, Page: Page("<index>")},
		"https://example.com/scioperi/1/": {Status: FetchModified, Page: Page("<notice 1 v1>")},
	}}
	var previous []ExtractedItem
	flow := &detailFlow{
		urls:   []string{"/scioperi/1/"},
		result: []ExtractedItem{{GUID: "n1", Title: "N1", Description: "v1"}},
		changes: func(prev, current []ExtractedItem) ([]ExtractedItem, error) {
			previous = prev
			return []ExtractedItem{{GUID: "n1", Title: "N1 updated", Description: "v2"}}, nil
		},
	}
	source := Source{ID: "tpl", URL: "https://example.com/scioperi/", Flow: flow}
	poller := Poller{Fetcher: fetcher, Store: store, Clock: fixedClock{now: time.Now()}}
	if _, err := poller.Poll(ctx, source); err != nil {
		t.Fatalf("first poll: %v", err)
	}

	fetcher.pages["https://example.com/scioperi/1/"] = FetchResult{Status: FetchModified, Page: Page("<notice 1 v2>")}
	flow.result = []ExtractedItem{{GUID: "n1", Title: "N1", Description: "v2"}}
	if _, err := poller.Poll(ctx, source); err != nil {
		t.Fatalf("second poll: %v", err)
	}
	if len(previous) != 1 || previous[0].Description != "v1" {
		t.Fatalf("Changes previous = %#v, want the stored v1 baseline", previous)
	}
	items, err := store.LoadSnapshot(ctx, "tpl")
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if len(items) != 1 || items[0].GUID != "tpl:1:n1" || items[0].Title != "N1 updated" {
		t.Fatalf("emitted items = %#v, want one revision-qualified change", items)
	}
}
