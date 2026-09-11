package rfs

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openRawSQLite(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

func historyItem(guid string, daysAgo int, replies int) Item {
	return Item{
		GUID:        guid,
		Title:       "Title " + guid,
		Link:        "https://example.org/thread/" + guid + "/",
		Description: "desc " + guid,
		PubDate:     time.Date(2026, 8, 20-daysAgo, 12, 0, 0, 0, time.UTC),
		Replies:     replies,
	}
}

func guids(items []Item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.GUID)
	}
	return out
}

func equalGUIDs(got []Item, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i].GUID != want[i] {
			return false
		}
	}
	return true
}

func TestMergeHistoryAccumulatesAcrossPolls(t *testing.T) {
	ctx := context.Background()
	store, err := OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	a := historyItem("ptg:1", 10, 200)
	if err := store.MergeHistory(ctx, "ptg", []Item{a}, []string{"ptg:1"}, 11); err != nil {
		t.Fatalf("merge poll1: %v", err)
	}
	b := historyItem("ptg:2", 0, 5)
	if err := store.MergeHistory(ctx, "ptg", []Item{b}, []string{"ptg:2"}, 11); err != nil {
		t.Fatalf("merge poll2: %v", err)
	}

	stored, err := store.LoadSnapshot(ctx, "ptg")
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if len(stored) != 2 {
		t.Fatalf("expected 2 accumulated rows, got %v", guids(stored))
	}
	live, err := store.LoadLiveGUIDs(ctx, "ptg")
	if err != nil {
		t.Fatalf("load live: %v", err)
	}
	if len(live) != 1 || live[0] != "ptg:2" {
		t.Fatalf("live = %v, want [ptg:2]", live)
	}
	// Dead thread visible; immature live hidden with minLive=100.
	visible, err := store.LoadVisibleHistory(ctx, "ptg", 100, 10)
	if err != nil {
		t.Fatalf("load visible: %v", err)
	}
	if !equalGUIDs(visible, []string{"ptg:1"}) {
		t.Fatalf("visible = %v, want [ptg:1]", guids(visible))
	}
}

func TestLoadVisibleHistoryShowsMatureLive(t *testing.T) {
	ctx := context.Background()
	store, err := OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	old := historyItem("ptg:1", 5, 300)
	live := historyItem("ptg:2", 0, 150)
	if err := store.MergeHistory(ctx, "ptg", []Item{old}, []string{"ptg:1"}, 11); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if err := store.MergeHistory(ctx, "ptg", []Item{live}, []string{"ptg:2"}, 11); err != nil {
		t.Fatalf("merge: %v", err)
	}
	// Re-observe live with grown replies: upsert must flip visibility.
	grown := historyItem("ptg:2", 0, 150)
	if err := store.MergeHistory(ctx, "ptg", []Item{grown}, []string{"ptg:2"}, 11); err != nil {
		t.Fatalf("merge grown: %v", err)
	}
	visible, err := store.LoadVisibleHistory(ctx, "ptg", 100, 10)
	if err != nil {
		t.Fatalf("load visible: %v", err)
	}
	if !equalGUIDs(visible, []string{"ptg:2", "ptg:1"}) {
		t.Fatalf("mature live should be served newest-first, got %v", guids(visible))
	}
}

func TestMergeHistoryPrunesBeyondStoredLimitButNeverLive(t *testing.T) {
	ctx := context.Background()
	store, err := OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	// Seed 12 dead threads + 1 live = 13 rows, keepStored=11 keeps newest 11 incl live.
	var seed []Item
	for i := 12; i >= 1; i-- {
		seed = append(seed, historyItem("ptg:"+itoa(i), i, 200))
	}
	if err := store.MergeHistory(ctx, "ptg", seed, []string{"ptg:1"}, 11); err != nil {
		t.Fatalf("seed merge: %v", err)
	}
	stored, err := store.LoadSnapshot(ctx, "ptg")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(stored) != 11 {
		t.Fatalf("stored = %d rows, want 11: %v", len(stored), guids(stored))
	}
	for _, it := range stored {
		if it.GUID == "ptg:12" {
			t.Fatalf("oldest should be pruned, still have %v", guids(stored))
		}
	}
	foundLive := false
	for _, it := range stored {
		if it.GUID == "ptg:1" {
			foundLive = true
		}
	}
	if !foundLive {
		t.Fatalf("live newest must be kept, got %v", guids(stored))
	}
	// Live is oldest-by-date edge: it must survive even beyond the window.
	ancient := Item{GUID: "ptg:live", Title: "T", Link: "https://example.com", Description: "d", PubDate: time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC), Replies: 1}
	if err := store.MergeHistory(ctx, "ptg", []Item{ancient}, []string{"ptg:live"}, 11); err != nil {
		t.Fatalf("merge ancient live: %v", err)
	}
	stored, err = store.LoadSnapshot(ctx, "ptg")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	found := false
	for _, it := range stored {
		if it.GUID == "ptg:live" {
			found = true
		}
	}
	if !found {
		t.Fatalf("live must never be pruned, got %v", guids(stored))
	}
}

func TestMergeHistoryHidesAllLiveDuringHandover(t *testing.T) {
	ctx := context.Background()
	store, err := OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	old := historyItem("ptg:1", 5, 400)
	b := historyItem("ptg:2", 1, 5)
	c := historyItem("ptg:3", 0, 7)
	if err := store.MergeHistory(ctx, "ptg", []Item{old, b, c}, []string{"ptg:2", "ptg:3"}, 11); err != nil {
		t.Fatalf("merge: %v", err)
	}
	visible, err := store.LoadVisibleHistory(ctx, "ptg", 100, 10)
	if err != nil {
		t.Fatalf("load visible: %v", err)
	}
	if !equalGUIDs(visible, []string{"ptg:1"}) {
		t.Fatalf("both live hidden during handover, got %v", guids(visible))
	}
}

func TestMergeHistoryMigratesPreRepliesDatabase(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := dir + "/old.sqlite"
	// Simulate a pre-feature database: snapshots without replies, no live_state.
	legacy, err := openRawSQLite(path)
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	for _, stmt := range []string{
		`CREATE TABLE snapshots (source_id TEXT NOT NULL, guid TEXT NOT NULL, title TEXT NOT NULL, link TEXT NOT NULL, description TEXT NOT NULL, pub_date TEXT NOT NULL, PRIMARY KEY (source_id, guid))`,
		`CREATE TABLE first_seen (source_id TEXT NOT NULL, guid TEXT NOT NULL, seen_at TEXT NOT NULL, PRIMARY KEY (source_id, guid))`,
		`CREATE TABLE fetch_cache (source_id TEXT PRIMARY KEY, etag TEXT NOT NULL DEFAULT '', last_modified TEXT NOT NULL DEFAULT '')`,
	} {
		if _, err := legacy.Exec(stmt); err != nil {
			legacy.Close()
			t.Fatalf("legacy ddl: %v", err)
		}
	}
	legacy.Close()
	store, err := OpenSQLiteStore(path)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	defer store.Close()
	if err := store.MergeHistory(ctx, "ptg", []Item{historyItem("ptg:1", 1, 5)}, []string{"ptg:1"}, 11); err != nil {
		t.Fatalf("merge after migration: %v", err)
	}
	visible, err := store.LoadVisibleHistory(ctx, "ptg", 100, 10)
	if err != nil {
		t.Fatalf("visible after migration: %v", err)
	}
	if len(visible) != 0 {
		t.Fatalf("immature live hidden after migration, got %v", guids(visible))
	}
}

func TestMergeHistoryUpsertsRepliesAndMetadata(t *testing.T) {
	ctx := context.Background()
	store, err := OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	first := historyItem("ptg:1", 1, 5)
	if err := store.MergeHistory(ctx, "ptg", []Item{first}, []string{"ptg:1"}, 11); err != nil {
		t.Fatalf("merge: %v", err)
	}
	updated := first
	updated.Title = "Title ptg:1 updated"
	updated.Replies = 250
	if err := store.MergeHistory(ctx, "ptg", []Item{updated}, []string{"ptg:1"}, 11); err != nil {
		t.Fatalf("remerge: %v", err)
	}
	visible, err := store.LoadVisibleHistory(ctx, "ptg", 100, 10)
	if err != nil {
		t.Fatalf("load visible: %v", err)
	}
	if len(visible) != 1 || visible[0].Replies != 250 || visible[0].Title != "Title ptg:1 updated" {
		t.Fatalf("upsert did not refresh row: %#v", visible)
	}
}
