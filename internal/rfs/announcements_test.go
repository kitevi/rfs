package rfs

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// announcementTestFlow announces every observed item exactly once, storing the
// announced GUIDs in its checkpoint. Assign items between polls to simulate a
// publisher adding a chapter.
type announcementTestFlow struct {
	items   []ExtractedItem
	version int
}

func (f *announcementTestFlow) Extract(Page) ([]ExtractedItem, error) { return f.items, nil }

func (f *announcementTestFlow) Version() int { return f.version }

func (f *announcementTestFlow) Evaluate(checkpoint json.RawMessage, current []ExtractedItem) (AnnouncementDecision, error) {
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

// compareAndAnnounceFlow implements both capabilities; the poller must reject
// an ambiguous Flow instead of silently picking one behavior.
type compareAndAnnounceFlow struct{ announcementTestFlow }

func (f *compareAndAnnounceFlow) Changes(_, current []ExtractedItem) ([]ExtractedItem, error) {
	return current, nil
}

// failingCommitStore fails announcement commits on demand while leaving stored
// state readable, so a test can observe what a failed poll left behind.
type failingCommitStore struct {
	*SQLiteStore
	fail bool
}

func (s *failingCommitStore) CommitAnnouncements(ctx context.Context, sourceID string, checkpoint json.RawMessage, items []Item, cache FetchCache) error {
	if s.fail {
		return fmt.Errorf("injected announcement commit failure")
	}
	return s.SQLiteStore.CommitAnnouncements(ctx, sourceID, checkpoint, items, cache)
}

func announcementItem(guid string) ExtractedItem {
	return ExtractedItem{GUID: guid, Title: "Chapter " + guid, Link: "https://example.com/" + guid}
}

func loadAnnouncementFeed(t *testing.T, store *SQLiteStore, sourceID string) map[string]Item {
	t.Helper()
	items, err := store.LoadSnapshot(context.Background(), sourceID)
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	stored := make(map[string]Item, len(items))
	for _, item := range items {
		stored[item.GUID] = item
	}
	return stored
}

func decodeAnnouncedGUIDs(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var announced []string
	if err := json.Unmarshal(raw, &announced); err != nil {
		t.Fatalf("decode checkpoint %q: %v", raw, err)
	}
	return announced
}

func TestAnnouncementFlowsAlwaysFetchTheFullPage(t *testing.T) {
	if !flowRequiresFullPage(&announcementTestFlow{}) {
		t.Fatal("announcement flow should always fetch the full page")
	}
}

func TestAnnouncementPollPublishesEachItemOnce(t *testing.T) {
	ctx := t.Context()
	store, err := OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	flow := &announcementTestFlow{items: []ExtractedItem{announcementItem("chapter:1")}, version: 1}
	source := Source{ID: "announcements", URL: "https://example.com/archive", Flow: flow}
	poller := Poller{Fetcher: emissionFetcher{}, Store: store}

	if _, err := poller.Poll(ctx, source); err != nil {
		t.Fatalf("first poll: %v", err)
	}
	flow.items = append(flow.items, announcementItem("chapter:2"))
	if _, err := poller.Poll(ctx, source); err != nil {
		t.Fatalf("second poll: %v", err)
	}
	if _, err := poller.Poll(ctx, source); err != nil {
		t.Fatalf("third poll: %v", err)
	}

	stored := loadAnnouncementFeed(t, store, source.ID)
	if len(stored) != 2 {
		t.Fatalf("stored %d items, want 2: %#v", len(stored), stored)
	}
	for _, guid := range []string{"chapter:1", "chapter:2"} {
		item, ok := stored[guid]
		if !ok {
			t.Fatalf("missing announced item %q", guid)
		}
		if item.GUID != guid {
			t.Fatalf("item GUID = %q, want the stable identity %q", item.GUID, guid)
		}
	}
	checkpoint, err := store.LoadAnnouncementCheckpoint(ctx, source.ID)
	if err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}
	if announced := decodeAnnouncedGUIDs(t, checkpoint); len(announced) != 2 {
		t.Fatalf("checkpoint announced %v, want both chapters", announced)
	}
}

func TestAnnouncementDedupSurvivesRestart(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "rfs.sqlite")
	flow := &announcementTestFlow{items: []ExtractedItem{announcementItem("chapter:1")}, version: 1}
	source := Source{ID: "announcements", URL: "https://example.com/archive", Flow: flow}

	store, err := OpenSQLiteStore(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if _, err := (Poller{Fetcher: emissionFetcher{}, Store: store}).Poll(ctx, source); err != nil {
		t.Fatalf("first poll: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	reopened, err := OpenSQLiteStore(path)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer reopened.Close()
	flow.items = append(flow.items, announcementItem("chapter:2"))
	result, err := (Poller{Fetcher: emissionFetcher{}, Store: reopened}).Poll(ctx, source)
	if err != nil {
		t.Fatalf("poll after restart: %v", err)
	}
	if result.Status != PollUpdated {
		t.Fatalf("poll after restart = %v, want updated", result.Status)
	}
	if stored := loadAnnouncementFeed(t, reopened, source.ID); len(stored) != 2 {
		t.Fatalf("stored %d items after restart, want 2", len(stored))
	}
	result, err = (Poller{Fetcher: emissionFetcher{}, Store: reopened}).Poll(ctx, source)
	if err != nil {
		t.Fatalf("repeat poll: %v", err)
	}
	if result.Status != PollUnchanged {
		t.Fatalf("repeat poll = %v, want unchanged", result.Status)
	}
	if stored := loadAnnouncementFeed(t, reopened, source.ID); len(stored) != 2 {
		t.Fatalf("repeat poll changed the feed: %#v", stored)
	}
}

func TestAnnouncementCommitFailurePreservesCheckpointAndFeed(t *testing.T) {
	ctx := t.Context()
	base, err := OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer base.Close()
	store := &failingCommitStore{SQLiteStore: base}
	flow := &announcementTestFlow{items: []ExtractedItem{announcementItem("chapter:1")}, version: 1}
	source := Source{ID: "announcements", URL: "https://example.com/archive", Flow: flow}
	poller := Poller{Fetcher: emissionFetcher{}, Store: store}

	if _, err := poller.Poll(ctx, source); err != nil {
		t.Fatalf("first poll: %v", err)
	}
	flow.items = append(flow.items, announcementItem("chapter:2"))
	store.fail = true
	if _, err := poller.Poll(ctx, source); err == nil {
		t.Fatal("poll with a failing commit did not fail")
	}
	stored := loadAnnouncementFeed(t, base, source.ID)
	if len(stored) != 1 {
		t.Fatalf("failed commit changed the feed: %#v", stored)
	}
	if _, ok := stored["chapter:2"]; ok {
		t.Fatal("failed commit published an announcement")
	}
	checkpoint, err := base.LoadAnnouncementCheckpoint(ctx, source.ID)
	if err != nil {
		t.Fatalf("load checkpoint: %v", err)
	}
	if announced := decodeAnnouncedGUIDs(t, checkpoint); len(announced) != 1 {
		t.Fatalf("failed commit advanced the checkpoint: %v", announced)
	}

	store.fail = false
	if _, err := poller.Poll(ctx, source); err != nil {
		t.Fatalf("recovery poll: %v", err)
	}
	if stored := loadAnnouncementFeed(t, base, source.ID); len(stored) != 2 {
		t.Fatalf("recovery poll stored %d items, want 2", len(stored))
	}
}

func TestAnnouncementFlowCannotAlsoCompareChanges(t *testing.T) {
	ctx := t.Context()
	store, err := OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	flow := &compareAndAnnounceFlow{announcementTestFlow{items: []ExtractedItem{announcementItem("chapter:1")}, version: 1}}
	poller := Poller{Fetcher: emissionFetcher{}, Store: store}
	_, err = poller.Poll(ctx, Source{ID: "ambiguous", URL: "https://example.com/archive", Flow: flow})
	if err == nil || !strings.Contains(err.Error(), "cannot both announce and compare changes") {
		t.Fatalf("poll error = %v, want an ambiguity rejection", err)
	}
}
