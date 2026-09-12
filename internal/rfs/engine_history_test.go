package rfs

import (
	"context"
	"errors"
	"testing"
	"time"
)

type histFetcher struct {
	result FetchResult
}

func (f *histFetcher) Fetch(_ context.Context, _ string, _ FetchCache) (FetchResult, error) {
	return f.result, nil
}

type histFlow struct {
	items   []ExtractedItem
	err     error
	version int
}

func (f *histFlow) Extract(_ Page) ([]ExtractedItem, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.items, nil
}
func (f *histFlow) Version() int { return f.version }

func TestPollerMergesHistoryInsteadOfReplacing(t *testing.T) {
	ctx := context.Background()
	store, err := OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	hist := &HistoryPolicy{VisibleLimit: 10, StoredLimit: 11, MinLiveReplies: 100}
	src := Source{ID: "ptg", URL: "https://example.com", Flow: &histFlow{items: []ExtractedItem{{GUID: "ptg:1", Title: "A", Link: "https://example.com/1", PubDate: &now, Replies: 200}}}, History: hist}
	poller := Poller{Fetcher: &histFetcher{result: FetchResult{Status: FetchModified, Page: emptyPage()}}, Store: store, Clock: fixedClock{now: now}}
	if _, err := poller.Poll(ctx, src); err != nil {
		t.Fatalf("poll1: %v", err)
	}
	later := now.Add(24 * time.Hour)
	src.Flow = &histFlow{items: []ExtractedItem{{GUID: "ptg:2", Title: "B", Link: "https://example.com/2", PubDate: &later, Replies: 5}}}
	poller.Clock = fixedClock{now: later}
	if _, err := poller.Poll(ctx, src); err != nil {
		t.Fatalf("poll2: %v", err)
	}
	stored, err := store.LoadSnapshot(ctx, "ptg")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(stored) != 2 {
		t.Fatalf("history should accumulate 2 rows, got %d", len(stored))
	}
	visible, err := store.LoadVisibleHistory(ctx, "ptg", 100, 10)
	if err != nil {
		t.Fatalf("visible: %v", err)
	}
	if len(visible) != 1 || visible[0].GUID != "ptg:1" {
		t.Fatalf("immature live ptg:2 hidden, want [ptg:1], got %v", visible)
	}
	if visible[0].Replies != 200 {
		t.Fatalf("replies not carried through poller: %#v", visible[0])
	}
}

func TestPollerPreservesLiveStateOnEmptySuccessfulObservation(t *testing.T) {
	ctx := context.Background()
	store, err := OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	seed := []Item{{GUID: "ptg:1", Title: "A", Link: "https://example.com/1", Description: "d", PubDate: now, Replies: 300}}
	if err := store.MergeHistory(ctx, "ptg", seed, []string{"ptg:1"}, 11); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// A parseable catalog gap yields zero items without an error; the poll
	// must preserve the stored live set instead of clearing it.
	src := Source{ID: "ptg", URL: "https://example.com", Flow: &histFlow{items: nil}, History: &HistoryPolicy{VisibleLimit: 10, StoredLimit: 11, MinLiveReplies: 100}}
	poller := Poller{Fetcher: &histFetcher{result: FetchResult{Status: FetchModified, Page: emptyPage()}}, Store: store, Clock: fixedClock{now: now}}
	if _, err := poller.Poll(ctx, src); err != nil {
		t.Fatalf("poll: %v", err)
	}
	live, err := store.LoadLiveGUIDs(ctx, "ptg")
	if err != nil || len(live) != 1 || live[0] != "ptg:1" {
		t.Fatalf("empty observation cleared live state, got %v", live)
	}
	visible, err := store.LoadVisibleHistory(ctx, "ptg", 100, 10)
	if err != nil {
		t.Fatalf("visible: %v", err)
	}
	if len(visible) != 1 || visible[0].GUID != "ptg:1" {
		t.Fatalf("empty observation dropped history, got %v", visible)
	}
}

func TestPollerPreservesHistoryOnExtractError(t *testing.T) {
	ctx := context.Background()
	store, err := OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	hist := &HistoryPolicy{VisibleLimit: 10, StoredLimit: 11, MinLiveReplies: 100}
	seed := []Item{{GUID: "ptg:1", Title: "A", Link: "https://example.com/1", Description: "d", PubDate: now, Replies: 300}}
	if err := store.MergeHistory(ctx, "ptg", seed, []string{}, 11); err != nil {
		t.Fatalf("seed: %v", err)
	}
	src := Source{ID: "ptg", URL: "https://example.com", Flow: &histFlow{err: errors.New("ptg: no matching threads")}, History: hist}
	poller := Poller{Fetcher: &histFetcher{result: FetchResult{Status: FetchModified, Page: emptyPage()}}, Store: store, Clock: fixedClock{now: now}}
	if _, err := poller.Poll(ctx, src); err == nil {
		t.Fatal("expected extract error")
	}
	visible, err := store.LoadVisibleHistory(ctx, "ptg", 100, 10)
	if err != nil {
		t.Fatalf("visible: %v", err)
	}
	if len(visible) != 1 || visible[0].GUID != "ptg:1" {
		t.Fatalf("rotation gap must preserve history, got %v", visible)
	}
}
