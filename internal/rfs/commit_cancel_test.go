package rfs

import (
	"context"
	"encoding/json"
	"testing"
)

// shutdownDuringCommitStore cancels the poll as each commit starts, the way a
// SIGTERM or a self-update arriving at that instant would.
type shutdownDuringCommitStore struct {
	*SQLiteStore
	cancel context.CancelFunc
}

func (s shutdownDuringCommitStore) SaveSnapshot(ctx context.Context, sourceID string, items []Item) error {
	s.cancel()
	return s.SQLiteStore.SaveSnapshot(ctx, sourceID, items)
}

func (s shutdownDuringCommitStore) CommitAnnouncements(ctx context.Context, sourceID string, checkpoint json.RawMessage, items []Item, cache FetchCache) error {
	s.cancel()
	return s.SQLiteStore.CommitAnnouncements(ctx, sourceID, checkpoint, items, cache)
}

type projectionStubFlow struct{ items []ExtractedItem }

func (f projectionStubFlow) Extract(Page) ([]ExtractedItem, error) { return f.items, nil }
func (projectionStubFlow) Version() int                            { return 1 }

func TestShutdownDuringCommitStillCommits(t *testing.T) {
	cases := []struct {
		name string
		flow Flow
	}{
		{"projection", projectionStubFlow{items: []ExtractedItem{announcementItem("1")}}},
		{"announcement", &announcementTestFlow{items: []ExtractedItem{announcementItem("1")}, version: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newEmissionStore(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			poller := Poller{
				Fetcher: emissionFetcher{},
				Store:   shutdownDuringCommitStore{SQLiteStore: store, cancel: cancel},
			}
			source := Source{ID: "s", URL: "https://example.com/", Flow: tc.flow}

			if _, err := poller.Poll(ctx, source); err != nil {
				t.Fatalf("poll interrupted by shutdown during its commit: %v", err)
			}
			if ctx.Err() == nil {
				t.Fatal("the store never simulated shutdown")
			}
			if feed := loadAnnouncementFeed(t, store, "s"); len(feed) != 1 {
				t.Fatalf("stored items = %d, want the committed item", len(feed))
			}
			cache, err := store.LoadFetchCache(context.Background(), "s")
			if err != nil {
				t.Fatalf("load fetch cache: %v", err)
			}
			if cache.ExtractVersion != 1 {
				t.Fatalf("extract version = %d, want 1: the validators did not commit", cache.ExtractVersion)
			}
		})
	}
}
