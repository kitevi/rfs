package rfs

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteStoreSavesAndLoadsSnapshotAndFetchCache(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	cache := FetchCache{ETag: `"v1"`, LastModified: "Wed, 07 Apr 1982 00:00:00 GMT"}
	if err := store.SaveFetchCache(ctx, "meltzer", cache); err != nil {
		t.Fatalf("save fetch cache: %v", err)
	}
	loadedCache, err := store.LoadFetchCache(ctx, "meltzer")
	if err != nil {
		t.Fatalf("load fetch cache: %v", err)
	}
	if loadedCache != cache {
		t.Fatalf("cache = %#v, want %#v", loadedCache, cache)
	}

	oldItem := Item{GUID: "old", Title: "Old", Link: "https://example.com/old", PubDate: time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)}
	newItem := Item{GUID: "new", Title: "New", Link: "https://example.com/new", PubDate: time.Date(1982, 4, 7, 0, 0, 0, 0, time.UTC)}
	if err := store.SaveSnapshot(ctx, "meltzer", []Item{oldItem, newItem}); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}

	items, err := store.LoadSnapshot(ctx, "meltzer")
	if err != nil {
		t.Fatalf("load snapshot: %v", err)
	}
	if len(items) != 2 || items[0].GUID != "new" || items[1].GUID != "old" {
		t.Fatalf("items not loaded newest-first: %#v", items)
	}

	if err := store.SaveSnapshot(ctx, "meltzer", []Item{newItem}); err != nil {
		t.Fatalf("replace snapshot: %v", err)
	}
	items, err = store.LoadSnapshot(ctx, "meltzer")
	if err != nil {
		t.Fatalf("load replaced snapshot: %v", err)
	}
	if len(items) != 1 || items[0].GUID != "new" {
		t.Fatalf("snapshot was not replaced: %#v", items)
	}
}

func TestSQLiteStoreFirstSeenIsStablePerSourceAndGUID(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	first := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	later := first.Add(24 * time.Hour)

	got, err := store.FirstSeen(ctx, "meltzer", "guid-1", first)
	if err != nil {
		t.Fatalf("first FirstSeen: %v", err)
	}
	if !got.Equal(first) {
		t.Fatalf("first seen = %s, want %s", got, first)
	}

	got, err = store.FirstSeen(ctx, "meltzer", "guid-1", later)
	if err != nil {
		t.Fatalf("second FirstSeen: %v", err)
	}
	if !got.Equal(first) {
		t.Fatalf("first seen changed: got %s want %s", got, first)
	}

	otherSource, err := store.FirstSeen(ctx, "other", "guid-1", later)
	if err != nil {
		t.Fatalf("other source FirstSeen: %v", err)
	}
	if !otherSource.Equal(later) {
		t.Fatalf("first seen should be source-scoped: got %s want %s", otherSource, later)
	}
}

func TestSQLiteStorePersistsExtractVersion(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	if err := store.SaveFetchCache(ctx, "s", FetchCache{ETag: "e", LastModified: "lm", ExtractVersion: 7}); err != nil {
		t.Fatalf("save fetch cache: %v", err)
	}
	got, err := store.LoadFetchCache(ctx, "s")
	if err != nil {
		t.Fatalf("load fetch cache: %v", err)
	}
	if got.ExtractVersion != 7 {
		t.Fatalf("extract version not persisted: %#v", got)
	}
}

func TestSQLiteStoreMigratesExistingFetchCacheToExtractVersion(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "rfs.sqlite")

	// Seed a database using the pre-version schema (no extract_version column).
	seed, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open seed db: %v", err)
	}
	if _, err := seed.Exec(`CREATE TABLE fetch_cache (
		source_id TEXT PRIMARY KEY,
		etag TEXT NOT NULL DEFAULT '',
		last_modified TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		t.Fatalf("create old fetch_cache: %v", err)
	}
	if _, err := seed.Exec(`INSERT INTO fetch_cache (source_id, etag, last_modified) VALUES (?, ?, ?)`, "meltzer", `"v1"`, "lm"); err != nil {
		t.Fatalf("seed row: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("close seed: %v", err)
	}

	// Opening through the store must add the column and preserve existing data.
	store, err := OpenSQLiteStore(path)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	defer store.Close()

	got, err := store.LoadFetchCache(ctx, "meltzer")
	if err != nil {
		t.Fatalf("load migrated cache: %v", err)
	}
	if got.ETag != `"v1"` || got.LastModified != "lm" || got.ExtractVersion != 0 {
		t.Fatalf("migration lost existing data or did not default version: %#v", got)
	}

	// The added column round-trips after migration.
	if err := store.SaveFetchCache(ctx, "meltzer", FetchCache{ETag: `"v2"`, LastModified: "lm2", ExtractVersion: 3}); err != nil {
		t.Fatalf("save after migration: %v", err)
	}
	got, err = store.LoadFetchCache(ctx, "meltzer")
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got.ExtractVersion != 3 || got.ETag != `"v2"` {
		t.Fatalf("version not persisted after migration: %#v", got)
	}

	// Reopening is idempotent: migration must not try to add the column twice.
	store2, err := OpenSQLiteStore(path)
	if err != nil {
		t.Fatalf("reopen migrated store: %v", err)
	}
	defer store2.Close()
}
