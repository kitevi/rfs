package rfs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// resolverFlow announces every observed item once, like the One Piece Flow,
// and resolves each new announcement through its own metadata URL.
type resolverFlow struct {
	items []ExtractedItem

	// base is the test server every metadata URL is built on.
	base string

	// rename makes enrichment return a different GUID, which rfs must reject.
	rename bool
}

func (f *resolverFlow) Extract(Page) ([]ExtractedItem, error) { return f.items, nil }

func (*resolverFlow) Version() int { return 1 }

func (f *resolverFlow) Evaluate(checkpoint json.RawMessage, current []ExtractedItem) (AnnouncementDecision, error) {
	announced := map[string]bool{}
	if len(checkpoint) > 0 {
		var stored []string
		if err := json.Unmarshal(checkpoint, &stored); err != nil {
			return AnnouncementDecision{}, err
		}
		for _, guid := range stored {
			announced[guid] = true
		}
	}
	var announcements []ExtractedItem
	for _, item := range current {
		if !announced[item.GUID] {
			announcements = append(announcements, item)
			announced[item.GUID] = true
		}
	}
	guids := make([]string, 0, len(announced))
	for guid := range announced {
		guids = append(guids, guid)
	}
	sort.Strings(guids)
	encoded, err := json.Marshal(guids)
	if err != nil {
		return AnnouncementDecision{}, err
	}
	return AnnouncementDecision{Checkpoint: encoded, Announcements: announcements}, nil
}

func (f *resolverFlow) AnnouncementEnrichmentURL(item ExtractedItem) (string, error) {
	if !strings.HasPrefix(item.GUID, "album:") {
		return "", errors.New("unexpected GUID")
	}
	return f.base + "/" + item.GUID + "/metadata", nil
}

func (f *resolverFlow) EnrichAnnouncement(page Page, item ExtractedItem) (ExtractedItem, error) {
	title := strings.TrimSpace(string(page))
	if title == "" {
		return ExtractedItem{}, errors.New("empty metadata")
	}
	item.Title = title
	if f.rename {
		item.GUID = item.GUID + "-renamed"
	}
	return item, nil
}

// metadataPlan controls per-album responses between polls.
type metadataPlan struct {
	failAt     int
	throttleAt int
}

// albumServer serves the source page plus one metadata URL per album and
// records every request path in order.
func albumServer(t *testing.T, plan *metadataPlan) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var requests []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requests = append(requests, r.URL.Path)
		mu.Unlock()
		if r.URL.Path == "/" {
			w.Write([]byte(`<page>`))
			return
		}
		id, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/album:"), "/metadata"))
		if err != nil {
			t.Errorf("unexpected metadata path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if plan.failAt == id {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if plan.throttleAt == id {
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprintf(w, "Album %d", id)
	}))
	t.Cleanup(server.Close)
	return server, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), requests...)
	}
}

// albums builds a numbered album batch.
func albums(count int) []ExtractedItem {
	items := make([]ExtractedItem, 0, count)
	for i := 1; i <= count; i++ {
		items = append(items, ExtractedItem{GUID: "album:" + strconv.Itoa(i), Link: "https://example.com/albums"})
	}
	return items
}

// newAlbumPoller wires a resolver Flow to a real fetcher and store.
func newAlbumPoller(t *testing.T, plan *metadataPlan) (*resolverFlow, Source, Poller, *SQLiteStore, func() []string) {
	t.Helper()
	server, requests := albumServer(t, plan)
	store, err := OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	flow := &resolverFlow{items: albums(2), base: server.URL}
	source := Source{ID: "albums", URL: server.URL, Flow: flow}
	return flow, source, Poller{Fetcher: NewHTTPFetcher(server.Client()), Store: store}, store, requests
}

func TestAnnouncementEnrichmentPublishesResolvedItemsOnlyOnce(t *testing.T) {
	ctx := t.Context()
	flow, source, poller, store, requests := newAlbumPoller(t, &metadataPlan{})

	result, err := poller.Poll(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != PollUpdated {
		t.Fatalf("first poll = %v, want updated", result.Status)
	}
	stored := loadAnnouncementFeed(t, store, source.ID)
	if len(stored) != 2 {
		t.Fatalf("stored %d items, want 2", len(stored))
	}
	if stored["album:1"].Title != "Album 1" || stored["album:2"].Title != "Album 2" {
		t.Fatalf("items were not enriched: %#v", stored)
	}
	if got := requests(); len(got) != 3 || got[1] != "/album:1/metadata" || got[2] != "/album:2/metadata" {
		t.Fatalf("first poll requests = %v, want the page plus one metadata fetch per album", got)
	}

	// An unchanged observation re-derives the page but resolves nothing again.
	flow.items = albums(2)
	if result, err = poller.Poll(ctx, source); err != nil {
		t.Fatal(err)
	}
	if result.Status != PollUnchanged {
		t.Fatalf("repeat poll = %v, want unchanged", result.Status)
	}
	if got := requests(); len(got) != 4 {
		t.Fatalf("repeat poll requests = %v, want only the page", got)
	}

	// A new album resolves only its own metadata.
	flow.items = albums(3)
	if result, err = poller.Poll(ctx, source); err != nil {
		t.Fatal(err)
	}
	if result.Status != PollUpdated {
		t.Fatalf("poll after a new album = %v, want updated", result.Status)
	}
	// The third poll re-derives the page and resolves only the new album.
	if got := requests(); len(got) != 6 || got[5] != "/album:3/metadata" {
		t.Fatalf("requests after a new album = %v, want one page and one metadata fetch more", got)
	}
	if stored = loadAnnouncementFeed(t, store, source.ID); len(stored) != 3 {
		t.Fatalf("stored %d items after a new album, want 3", len(stored))
	}
}

func TestAnnouncementEnrichmentFailurePublishesNothing(t *testing.T) {
	ctx := t.Context()
	plan := &metadataPlan{failAt: 2}
	_, source, poller, store, requests := newAlbumPoller(t, plan)

	if _, err := poller.Poll(ctx, source); err == nil {
		t.Fatal("poll with a failing player did not fail")
	}
	if stored := loadAnnouncementFeed(t, store, source.ID); len(stored) != 0 {
		t.Fatalf("failed poll published %#v", stored)
	}
	checkpoint, err := store.LoadAnnouncementCheckpoint(ctx, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(checkpoint) != 0 {
		t.Fatalf("failed poll advanced the checkpoint: %s", checkpoint)
	}

	plan.failAt = 0
	if _, err := poller.Poll(ctx, source); err != nil {
		t.Fatal(err)
	}
	stored := loadAnnouncementFeed(t, store, source.ID)
	if len(stored) != 2 || stored["album:1"].Title != "Album 1" {
		t.Fatalf("retry stored %#v, want both albums resolved", stored)
	}
	attempts := 0
	for _, request := range requests() {
		if request == "/album:1/metadata" {
			attempts++
		}
	}
	if attempts != 2 {
		t.Fatalf("first album was resolved %d times, want a retry after the failed batch", attempts)
	}
}

func TestAnnouncementEnrichmentPropagatesThrottling(t *testing.T) {
	ctx := t.Context()
	plan := &metadataPlan{throttleAt: 2}
	_, source, poller, store, _ := newAlbumPoller(t, plan)

	result, err := poller.Poll(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != PollThrottled || result.RetryAfter != 2*time.Minute {
		t.Fatalf("throttled poll = %+v, want throttled with Retry-After", result)
	}
	if stored := loadAnnouncementFeed(t, store, source.ID); len(stored) != 0 {
		t.Fatalf("throttled poll published %#v", stored)
	}

	plan.throttleAt = 0
	if _, err := poller.Poll(ctx, source); err != nil {
		t.Fatal(err)
	}
	if stored := loadAnnouncementFeed(t, store, source.ID); len(stored) != 2 {
		t.Fatalf("recovered poll stored %d items, want 2", len(stored))
	}
}

func TestAnnouncementEnrichmentStopsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, source, poller, store, _ := newAlbumPoller(t, &metadataPlan{})

	if _, err := poller.Poll(ctx, source); err == nil {
		t.Fatal("cancelled poll did not fail")
	}
	if stored := loadAnnouncementFeed(t, store, source.ID); len(stored) != 0 {
		t.Fatalf("cancelled poll published %#v", stored)
	}
}

func TestAnnouncementEnrichmentCannotRenameAnIdentity(t *testing.T) {
	ctx := t.Context()
	flow, source, poller, store, _ := newAlbumPoller(t, &metadataPlan{})
	flow.rename = true

	_, err := poller.Poll(ctx, source)
	if err == nil || !strings.Contains(err.Error(), "GUID") {
		t.Fatalf("poll error = %v, want a GUID invariant rejection", err)
	}
	if stored := loadAnnouncementFeed(t, store, source.ID); len(stored) != 0 {
		t.Fatalf("renaming poll published %#v", stored)
	}
}

// partialFlow leaves items whose GUID ends in 2 unresolved.
type partialFlow struct{ *resolverFlow }

func (f *partialFlow) AnnouncementEnrichmentURL(item ExtractedItem) (string, error) {
	if strings.HasSuffix(item.GUID, "2") {
		return "", nil
	}
	return f.resolverFlow.AnnouncementEnrichmentURL(item)
}

func TestAnnouncementEnrichmentSkipsItemsWithoutAMetadataURL(t *testing.T) {
	ctx := t.Context()
	server, requests := albumServer(t, &metadataPlan{})
	store, err := OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	flow := &partialFlow{resolverFlow: &resolverFlow{items: albums(2), base: server.URL}}
	source := Source{ID: "albums", URL: server.URL, Flow: flow}
	poller := Poller{Fetcher: NewHTTPFetcher(server.Client()), Store: store}

	if _, err := poller.Poll(ctx, source); err != nil {
		t.Fatal(err)
	}
	stored := loadAnnouncementFeed(t, store, source.ID)
	if len(stored) != 2 {
		t.Fatalf("stored %d items, want 2", len(stored))
	}
	if stored["album:1"].Title != "Album 1" {
		t.Fatalf("resolved item = %#v", stored["album:1"])
	}
	if stored["album:2"].Title != "" {
		t.Fatalf("unresolved item was changed: %#v", stored["album:2"])
	}
	metadata := 0
	for _, request := range requests() {
		if strings.HasSuffix(request, "/metadata") {
			metadata++
		}
	}
	if metadata != 1 {
		t.Fatalf("metadata requests = %d, want only the resolvable item", metadata)
	}
}

func TestAnnouncementEnrichmentResolvesLargeBatches(t *testing.T) {
	ctx := t.Context()
	server, _ := albumServer(t, &metadataPlan{})
	store, err := OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	flow := &resolverFlow{items: albums(60), base: server.URL}
	source := Source{ID: "albums", URL: server.URL, Flow: flow}
	poller := Poller{Fetcher: NewHTTPFetcher(server.Client()), Store: store}

	if _, err := poller.Poll(ctx, source); err != nil {
		t.Fatal(err)
	}
	stored := loadAnnouncementFeed(t, store, source.ID)
	if len(stored) != 60 {
		t.Fatalf("stored %d items, want the whole batch", len(stored))
	}
	if stored["album:60"].Title != "Album 60" {
		t.Fatalf("last album = %#v", stored["album:60"])
	}
}
