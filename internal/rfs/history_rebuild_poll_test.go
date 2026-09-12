package rfs_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/kitevi/rfs/internal/rfs"
	"github.com/kitevi/rfs/internal/sources/ptg"
)

type pollFetcher struct{ result rfs.FetchResult }

func (f pollFetcher) Fetch(context.Context, string, rfs.FetchCache) (rfs.FetchResult, error) {
	return f.result, nil
}

type pollClock struct{ now time.Time }

func (c pollClock) Now() time.Time { return c.now }

// plainFlow has no RebuildStored, like the change feeds and snapshot-replace flows.
type plainFlow struct{ version int }

func (f plainFlow) Extract(rfs.Page) ([]rfs.ExtractedItem, error) { return nil, nil }
func (f plainFlow) Version() int                                  { return f.version }

const ptgCatalogPage = "[{\"page\":1,\"threads\":[{\"no\":109765454,\"sub\":\"/ptg/ - Private Trackers General\",\"com\":\"clowning on these neons Edition\",\"time\":1788200342,\"replies\":150}]}]"

func seedStaleTitles(t *testing.T, store *rfs.SQLiteStore, version int) {
	t.Helper()
	ctx := context.Background()
	old := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	rows := []rfs.Item{
		{GUID: "ptg:109732075", Title: "/ptg/ - Private Trackers General \u2014 wired edition", Link: "l", Description: "d", PubDate: old, Replies: 400},
		{GUID: "ptg:109700000", Title: "/ptg/ - Private Trackers General \u2014 older edition", Link: "l", Description: "d", PubDate: old.Add(-72 * time.Hour), Replies: 400},
	}
	if err := store.MergeHistory(ctx, "ptg", rows, []string{}, 11); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := store.SaveFetchCache(ctx, "ptg", rfs.FetchCache{ETag: "old", LastModified: "lm", ExtractVersion: version}); err != nil {
		t.Fatalf("cache: %v", err)
	}
}

func pollPTG(t *testing.T, store *rfs.SQLiteStore, flow rfs.Flow) {
	t.Helper()
	src := rfs.Source{ID: "ptg", URL: ptg.PageURL, Flow: flow, History: rfs.DefaultCatalogHistory()}
	poller := rfs.Poller{
		Fetcher: pollFetcher{result: rfs.FetchResult{Status: rfs.FetchModified, Page: rfs.Page(ptgCatalogPage)}},
		Store:   store,
		Clock:   pollClock{now: time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)},
	}
	if _, err := poller.Poll(context.Background(), src); err != nil {
		t.Fatalf("poll: %v", err)
	}
}

func stalePrefixCount(t *testing.T, store *rfs.SQLiteStore) []string {
	t.Helper()
	visible, err := store.LoadVisibleHistory(context.Background(), "ptg", 100, 10)
	if err != nil {
		t.Fatalf("visible: %v", err)
	}
	var stale []string
	for _, it := range visible {
		if strings.HasPrefix(it.Title, "/ptg/") {
			stale = append(stale, it.Title)
		}
	}
	return stale
}

// The version bump must rewrite rows the catalog no longer lists.
func TestPollRepairsLegacyTitlesOnExtractVersionChange(t *testing.T) {
	store, err := rfs.OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	seedStaleTitles(t, store, ptg.ExtractVersion-1)
	pollPTG(t, store, ptg.Flow{})

	visible, err := store.LoadVisibleHistory(context.Background(), "ptg", 100, 10)
	if err != nil {
		t.Fatalf("visible: %v", err)
	}
	if len(visible) != 3 {
		t.Fatalf("expected 3 visible rows, got %d", len(visible))
	}
	if stale := stalePrefixCount(t, store); len(stale) != 0 {
		t.Fatalf("stored rows still carry the stripped prefix: %q", stale)
	}
}

// The rebuild is gated on the version change.
func TestPollSkipsRebuildWhenVersionUnchanged(t *testing.T) {
	store, err := rfs.OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	seedStaleTitles(t, store, ptg.ExtractVersion)
	pollPTG(t, store, ptg.Flow{})

	if stale := stalePrefixCount(t, store); len(stale) != 2 {
		t.Fatalf("unchanged version must not rebuild, got %d rows left", len(stale))
	}
}

// A flow without a rebuilder leaves stored titles untouched on a bump.
func TestPollLeavesTitlesForFlowWithoutRebuilder(t *testing.T) {
	store, err := rfs.OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	seedStaleTitles(t, store, 1)
	pollPTG(t, store, plainFlow{version: 2})

	if stale := stalePrefixCount(t, store); len(stale) != 2 {
		t.Fatalf("a flow without HistoryRebuilder must not rewrite titles, got %d", 2-len(stale))
	}
}
