package rfs

import (
	"context"
	"testing"
	"time"
)

func TestPollerStoresExtractedSnapshotAndAppliesFirstSeenFallback(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	sourceDate := time.Date(1982, 4, 7, 0, 0, 0, 0, time.UTC)
	fetcher := &fakeFetcher{result: FetchResult{Status: FetchModified, Page: emptyPage(), Cache: FetchCache{ETag: `"v1"`}}}
	flow := &testFlow{items: []ExtractedItem{
		{GUID: "with-source-date", Title: "With source date", Link: "https://example.com/a", PubDate: &sourceDate},
		{GUID: "needs-first-seen", Title: "Needs first seen", Link: "https://example.com/b"},
	}}
	poller := Poller{Fetcher: fetcher, Store: store, Clock: fixedClock{now: now}}

	result, err := poller.Poll(ctx, Source{ID: "meltzer", URL: "https://example.com", Flow: flow})
	if err != nil {
		t.Fatalf("Poll returned error: %v", err)
	}
	if result.Status != PollUpdated {
		t.Fatalf("expected updated status, got %v", result.Status)
	}
	if fetcher.cache != (FetchCache{}) {
		t.Fatalf("expected first fetch to use empty cache, got %#v", fetcher.cache)
	}

	items, err := store.LoadSnapshot(ctx, "meltzer")
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 stored items, got %#v", items)
	}
	byGUID := map[string]Item{}
	for _, item := range items {
		byGUID[item.GUID] = item
	}
	if !byGUID["with-source-date"].PubDate.Equal(sourceDate) {
		t.Fatalf("source-derived pubDate was not kept: %#v", byGUID["with-source-date"])
	}
	if !byGUID["needs-first-seen"].PubDate.Equal(now) {
		t.Fatalf("first-seen fallback was not applied: %#v", byGUID["needs-first-seen"])
	}

	cache, err := store.LoadFetchCache(ctx, "meltzer")
	if err != nil {
		t.Fatalf("load fetch cache: %v", err)
	}
	if cache.ETag != `"v1"` {
		t.Fatalf("fetch cache was not saved: %#v", cache)
	}
}

func TestPollerDeduplicatesExtractedGUIDsBeforeSavingSnapshot(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	pubDate := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	fetcher := &fakeFetcher{result: FetchResult{Status: FetchModified, Page: emptyPage()}}
	flow := &testFlow{items: []ExtractedItem{
		{GUID: "duplicate", Title: "First", Link: "https://example.com/a", PubDate: &pubDate},
		{GUID: "duplicate", Title: "Second", Link: "https://example.com/b", PubDate: &pubDate},
	}}
	poller := Poller{Fetcher: fetcher, Store: store, Clock: fixedClock{now: pubDate}}

	_, err := poller.Poll(ctx, Source{ID: "meltzer", URL: "https://example.com", Flow: flow})
	if err != nil {
		t.Fatalf("Poll returned error: %v", err)
	}

	items, err := store.LoadSnapshot(ctx, "meltzer")
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if len(items) != 1 || items[0].GUID != "duplicate" || items[0].Title != "First" {
		t.Fatalf("snapshot was not deduplicated keeping first item: %#v", items)
	}
}

func TestPollerSkipsFlowWhenSourceNotModified(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	if err := store.SaveFetchCache(ctx, "meltzer", FetchCache{ETag: `"v1"`}); err != nil {
		t.Fatalf("save cache: %v", err)
	}

	flow := &testFlow{}
	fetcher := &fakeFetcher{result: FetchResult{Status: FetchNotModified, Cache: FetchCache{ETag: `"v1"`}}}
	poller := Poller{Fetcher: fetcher, Store: store, Clock: fixedClock{now: time.Now()}}

	result, err := poller.Poll(ctx, Source{ID: "meltzer", URL: "https://example.com", Flow: flow})
	if err != nil {
		t.Fatalf("Poll returned error: %v", err)
	}
	if result.Status != PollUnchanged {
		t.Fatalf("expected unchanged status, got %v", result.Status)
	}
	if flow.called {
		t.Fatal("flow should not be called for not-modified source")
	}
	if fetcher.cache.ETag != `"v1"` {
		t.Fatalf("poller did not pass stored fetch cache: %#v", fetcher.cache)
	}
}

type fakeFetcher struct {
	result FetchResult
	cache  FetchCache
}

func (f *fakeFetcher) Fetch(ctx context.Context, url string, cache FetchCache) (FetchResult, error) {
	f.cache = cache
	return f.result, nil
}

// testFlow is the minimal Flow the poller tests need: fixed items or an
// error, a version, and a record of whether Extract ran.
type testFlow struct {
	items   []ExtractedItem
	err     error
	called  bool
	version int
}

func (f *testFlow) Extract(Page) ([]ExtractedItem, error) {
	f.called = true
	if f.err != nil {
		return nil, f.err
	}
	return f.items, nil
}

func (f *testFlow) Version() int { return f.version }

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func TestPollerForcesUnconditionalFetchWhenFlowVersionAdvances(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	// The stored snapshot was derived by version 1; the running Flow is version 2.
	if err := store.SaveFetchCache(ctx, "meltzer", FetchCache{ETag: `"v1"`, ExtractVersion: 1}); err != nil {
		t.Fatalf("save fetch cache: %v", err)
	}
	now := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	fetcher := &fakeFetcher{result: FetchResult{
		Status: FetchModified,
		Page:   emptyPage(),
		Cache:  FetchCache{ETag: `"v2"`},
	}}
	flow := &testFlow{
		items:   []ExtractedItem{{GUID: "g", Title: "T", Link: "https://example.com", PubDate: &now}},
		version: 2,
	}
	poller := Poller{Fetcher: fetcher, Store: store, Clock: fixedClock{now: now}}

	result, err := poller.Poll(ctx, Source{ID: "meltzer", URL: "https://example.com", Flow: flow})
	if err != nil {
		t.Fatalf("Poll returned error: %v", err)
	}
	if result.Status != PollUpdated {
		t.Fatalf("expected PollUpdated, got %v", result.Status)
	}
	// A version mismatch must drop the stored conditional headers so the server
	// is forced to return the full page (200) instead of a stale 304.
	if fetcher.cache.ETag != "" || fetcher.cache.LastModified != "" {
		t.Fatalf("expected unconditional fetch on version advance, got %#v", fetcher.cache)
	}
	cache, err := store.LoadFetchCache(ctx, "meltzer")
	if err != nil {
		t.Fatalf("load fetch cache: %v", err)
	}
	if cache.ETag != `"v2"` {
		t.Fatalf("response ETag not persisted: %#v", cache)
	}
	if cache.ExtractVersion != 2 {
		t.Fatalf("extract version not advanced to running Flow's version: %#v", cache)
	}
}

func TestPollerKeepsConditionalFetchWhenFlowVersionMatches(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	if err := store.SaveFetchCache(ctx, "meltzer", FetchCache{ETag: `"v1"`, ExtractVersion: 1}); err != nil {
		t.Fatalf("save fetch cache: %v", err)
	}
	flow := &testFlow{version: 1}
	fetcher := &fakeFetcher{result: FetchResult{Status: FetchNotModified, Cache: FetchCache{ETag: `"v1"`}}}
	poller := Poller{Fetcher: fetcher, Store: store, Clock: fixedClock{now: time.Now()}}

	result, err := poller.Poll(ctx, Source{ID: "meltzer", URL: "https://example.com", Flow: flow})
	if err != nil {
		t.Fatalf("Poll returned error: %v", err)
	}
	if result.Status != PollUnchanged {
		t.Fatalf("expected PollUnchanged, got %v", result.Status)
	}
	if flow.called {
		t.Fatal("Flow.Extract must not run when the page is unchanged and the version matches")
	}
	// Matching version keeps the cheap conditional fast-path: the stored ETag is
	// sent and the 304 is honored.
	if fetcher.cache.ETag != `"v1"` {
		t.Fatalf("conditional headers dropped despite matching version: %#v", fetcher.cache)
	}
	cache, err := store.LoadFetchCache(ctx, "meltzer")
	if err != nil {
		t.Fatalf("load fetch cache: %v", err)
	}
	if cache.ExtractVersion != 1 {
		t.Fatalf("extract version clobbered to 0 by a 304 with no version header: %#v", cache)
	}
}
func emptyPage() Page {
	return Page(`<html><body></body></html>`)
}
