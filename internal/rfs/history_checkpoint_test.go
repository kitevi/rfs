package rfs

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestHistoryCheckpointFailureRollsBack(t *testing.T) {
	ctx := context.Background()
	store, err := OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	old := Item{GUID: "old", Title: "old", PubDate: time.Unix(1000, 0).UTC()}
	if err := store.MergeHistory(ctx, "s", []Item{old}, []string{"old"}, 11); err != nil {
		t.Fatal(err)
	}
	prior := FetchCache{ETag: "prior", ExtractVersion: 1}
	if err := store.SaveFetchCache(ctx, "s", prior); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`CREATE TRIGGER reject_checkpoint BEFORE UPDATE ON fetch_cache BEGIN SELECT RAISE(ABORT, 'checkpoint failed'); END`); err != nil {
		t.Fatal(err)
	}
	fresh := Item{GUID: "new", Title: "new", PubDate: old.PubDate.Add(time.Hour)}
	err = store.CommitHistory(ctx, "s", []Item{fresh}, []string{"new"}, 1, FetchCache{ExtractVersion: 2}, func(item Item) (Item, error) { item.Title = "rebuilt"; return item, nil })
	if err == nil {
		t.Fatal("expected checkpoint failure")
	}
	stored, err := store.LoadSnapshot(ctx, "s")
	if err != nil || !reflect.DeepEqual(stored, []Item{old}) {
		t.Fatalf("snapshot changed: %#v %v", stored, err)
	}
	live, err := store.LoadLiveGUIDs(ctx, "s")
	if err != nil || !reflect.DeepEqual(live, []string{"old"}) {
		t.Fatalf("live state changed: %v %v", live, err)
	}
	cache, err := store.LoadFetchCache(ctx, "s")
	if err != nil || cache != prior {
		t.Fatalf("checkpoint changed: %#v %v", cache, err)
	}
}
