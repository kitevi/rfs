package rfs

import (
	"context"
	"testing"
	"time"
)

// emissionFetcher always serves one page, so a poll only depends on the Flow.
type emissionFetcher struct{}

func (emissionFetcher) Fetch(_ context.Context, _ string, _ FetchCache) (FetchResult, error) {
	return FetchResult{Status: FetchModified, Page: Page("<page>")}, nil
}

// emissionFlow diffs items by GUID and description and labels every difference.
type emissionFlow struct {
	items   []ExtractedItem
	version int
}

func (f *emissionFlow) Extract(Page) ([]ExtractedItem, error) { return f.items, nil }
func (f *emissionFlow) Version() int                          { return f.version }

func (f *emissionFlow) Changes(previous, current []ExtractedItem) ([]ExtractedItem, error) {
	old := make(map[string]ExtractedItem, len(previous))
	for _, item := range previous {
		old[item.GUID] = item
	}
	var changes []ExtractedItem
	for _, item := range current {
		if prior, ok := old[item.GUID]; ok && prior.Description == item.Description {
			continue
		}
		item.Title = "changed " + item.GUID
		changes = append(changes, item)
	}
	return changes, nil
}

func newEmissionStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestPollerPublishesFirstObservationWhenInitialEmissionEnabled(t *testing.T) {
	ctx := context.Background()
	store := newEmissionStore(t)
	flow := &emissionFlow{items: []ExtractedItem{{GUID: "a", Title: "A", Description: "state-a"}}, version: 1}
	source := Source{ID: "notices", URL: "https://example.com/feed", Flow: flow, EmitInitial: true}
	poller := Poller{Fetcher: emissionFetcher{}, Store: store, Clock: fixedClock{now: time.Now()}}

	if _, err := poller.Poll(ctx, source); err != nil {
		t.Fatalf("first poll: %v", err)
	}
	items, err := store.LoadSnapshot(ctx, "notices")
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if len(items) != 1 || items[0].GUID != "notices:1:a" || items[0].Title != "changed a" {
		t.Fatalf("first observation published %#v, want one announcement", items)
	}

	// A restart re-polls the same unchanged observation through a new Poller.
	restarted := Poller{Fetcher: emissionFetcher{}, Store: store, Clock: fixedClock{now: time.Now()}}
	if _, err := restarted.Poll(ctx, source); err != nil {
		t.Fatalf("restart poll: %v", err)
	}
	items, err = store.LoadSnapshot(ctx, "notices")
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("restart duplicated the first observation: %#v", items)
	}
}

func TestPollerKeepsFirstObservationSilentByDefault(t *testing.T) {
	ctx := context.Background()
	store := newEmissionStore(t)
	flow := &emissionFlow{items: []ExtractedItem{{GUID: "a", Title: "A", Description: "state-a"}}, version: 1}
	source := Source{ID: "seadex", URL: "https://example.com/feed", Flow: flow}
	poller := Poller{Fetcher: emissionFetcher{}, Store: store, Clock: fixedClock{now: time.Now()}}

	if _, err := poller.Poll(ctx, source); err != nil {
		t.Fatalf("poll: %v", err)
	}
	items, err := store.LoadSnapshot(ctx, "seadex")
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("default change feed emitted %#v, want a silent baseline", items)
	}
	state, err := store.LoadChangeState(ctx, "seadex")
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if !state.Initialized || len(state.Items) != 1 {
		t.Fatalf("state = %#v, want an initialized baseline", state)
	}
}

func TestPollerRebaselinesSilentlyOnVersionBump(t *testing.T) {
	ctx := context.Background()
	store := newEmissionStore(t)
	flow := &emissionFlow{items: []ExtractedItem{{GUID: "a", Title: "A", Description: "state-a"}}, version: 1}
	source := Source{ID: "notices", URL: "https://example.com/feed", Flow: flow, EmitInitial: true}
	poller := Poller{Fetcher: emissionFetcher{}, Store: store, Clock: fixedClock{now: time.Now()}}
	if _, err := poller.Poll(ctx, source); err != nil {
		t.Fatalf("first poll: %v", err)
	}

	// The Flow's derivations change while the upstream state does not.
	flow.version = 2
	flow.items = []ExtractedItem{{GUID: "a", Title: "A", Description: "state-a-v2"}}
	if _, err := poller.Poll(ctx, source); err != nil {
		t.Fatalf("version bump poll: %v", err)
	}
	items, err := store.LoadSnapshot(ctx, "notices")
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("version bump emitted %#v, want a silent rebaseline", items)
	}
	state, err := store.LoadChangeState(ctx, "notices")
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if state.Version != 2 || state.Items[0].Description != "state-a-v2" {
		t.Fatalf("state = %#v, want the rebaselined v2 payload", state)
	}
}

func TestPollerAnnouncesNewlyCoveredItemsAcrossAnExtractVersionChange(t *testing.T) {
	ctx := context.Background()
	store := newEmissionStore(t)
	flow := &emissionFlow{items: []ExtractedItem{{GUID: "a", Title: "A", Description: "state-a"}}, version: 1}
	source := Source{ID: "notices", URL: "https://example.com/feed", Flow: flow, EmitInitial: true, EmitVersionChanges: true}
	poller := Poller{Fetcher: emissionFetcher{}, Store: store, Clock: fixedClock{now: time.Now()}}
	if _, err := poller.Poll(ctx, source); err != nil {
		t.Fatalf("first poll: %v", err)
	}

	// The Flow widens its scope while the upstream state of the known item
	// stays exactly as it was.
	flow.version = 2
	flow.items = []ExtractedItem{
		{GUID: "a", Title: "A", Description: "state-a"},
		{GUID: "b", Title: "B", Description: "state-b"},
	}
	if _, err := poller.Poll(ctx, source); err != nil {
		t.Fatalf("version change poll: %v", err)
	}
	items, err := store.LoadSnapshot(ctx, "notices")
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	want := []string{"notices:1:a", "notices:2:b"}
	if len(items) != len(want) {
		t.Fatalf("emitted %#v, want the first observation and the newly covered item", items)
	}
	for i, guid := range want {
		if items[i].GUID != guid {
			t.Fatalf("item %d GUID = %q, want %q", i, items[i].GUID, guid)
		}
	}
}

func TestPollerEmitsDistinctRevisionGUIDsForRepeatedTransitions(t *testing.T) {
	ctx := context.Background()
	store := newEmissionStore(t)
	flow := &emissionFlow{items: []ExtractedItem{{GUID: "a", Title: "A", Description: "a"}}, version: 1}
	source := Source{ID: "notices", URL: "https://example.com/feed", Flow: flow, EmitInitial: true}
	poller := Poller{Fetcher: emissionFetcher{}, Store: store, Clock: fixedClock{now: time.Now()}}
	for _, state := range []string{"a", "b", "a"} {
		flow.items = []ExtractedItem{{GUID: "a", Title: "A", Description: state}}
		if _, err := poller.Poll(ctx, source); err != nil {
			t.Fatalf("poll %s: %v", state, err)
		}
	}
	items, err := store.LoadSnapshot(ctx, "notices")
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	want := []string{"notices:1:a", "notices:2:a", "notices:3:a"}
	if len(items) != len(want) {
		t.Fatalf("emitted %#v, want %d revision items", items, len(want))
	}
	for i, guid := range want {
		if items[i].GUID != guid {
			t.Fatalf("item %d GUID = %q, want %q", i, items[i].GUID, guid)
		}
	}
}

// clockedEmissionFlow is an emissionFlow that also implements
// ClockedChangeFlow, recording the instant it was asked to compare at and
// stamping emitted items with a Flow-supplied publication date.
type clockedEmissionFlow struct {
	emissionFlow
	at      time.Time
	pubDate *time.Time
}

func (f *clockedEmissionFlow) ChangesAt(at time.Time, previous, current []ExtractedItem) ([]ExtractedItem, error) {
	f.at = at
	changes, err := f.emissionFlow.Changes(previous, current)
	if err != nil || f.pubDate == nil {
		return changes, err
	}
	for i := range changes {
		changes[i].PubDate = f.pubDate
	}
	return changes, nil
}

func TestPollerComparesAtTheClockInstantAndHonorsExtractedPubDate(t *testing.T) {
	ctx := context.Background()
	store := newEmissionStore(t)
	published := time.Date(2026, 9, 4, 10, 6, 44, 0, time.UTC)
	flow := &clockedEmissionFlow{pubDate: &published}
	flow.items = []ExtractedItem{{GUID: "a", Title: "A", Description: "state-a"}}
	flow.version = 1
	source := Source{ID: "notices", URL: "https://example.com/feed", Flow: flow, EmitInitial: true}
	at := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	poller := Poller{Fetcher: emissionFetcher{}, Store: store, Clock: fixedClock{now: at}}

	if _, err := poller.Poll(ctx, source); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if !flow.at.Equal(at) {
		t.Fatalf("comparison ran at %v, want the injected clock instant %v", flow.at, at)
	}
	items, err := store.LoadSnapshot(ctx, "notices")
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("published %#v, want one item", items)
	}
	if !items[0].PubDate.Equal(published) {
		t.Fatalf("pubDate = %v, want the notice's own publication time %v", items[0].PubDate, published)
	}
	if items[0].GUID != "notices:1:a" {
		t.Fatalf("GUID = %q, want the revision GUID", items[0].GUID)
	}
}
