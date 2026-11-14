package seadex_test

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/kitevi/rfs/internal/rfs"
	"github.com/kitevi/rfs/internal/rfstest"
	"github.com/kitevi/rfs/internal/sources"
)

// Run explicitly on the deployment host; never contacts upstream in normal tests.
func TestLiveSeaDex(t *testing.T) {
	if os.Getenv("RFS_TEST_SEADEX_LIVE") != "1" {
		t.Skip("set RFS_TEST_SEADEX_LIVE=1 to probe SeaDex and AniList")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	store := rfstest.Store(t)
	p := rfs.Poller{Fetcher: rfs.NewHTTPFetcher(&http.Client{Timeout: 30 * time.Second}), Store: store}
	for _, source := range sources.All() {
		if source.ID != "seadex" {
			continue
		}
		result, err := p.Poll(ctx, source)
		if err != nil {
			t.Fatal(err)
		}
		if result.Status == rfs.PollThrottled {
			t.Fatalf("upstream throttled: retry after %s", result.RetryAfter)
		}
		state, err := store.LoadChangeState(ctx, source.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !state.Initialized || len(state.Items) == 0 {
			t.Fatal("no populated baseline")
		}
		t.Logf("baseline saved: %d anime; an empty initial feed is expected", len(state.Items))
		flow := source.Flow.(rfs.EnrichedChangeFlow)
		url, err := flow.EnrichmentURL(state.Items[:1])
		if err != nil {
			t.Fatal(err)
		}
		metadata, err := p.Fetcher.Fetch(ctx, url, rfs.FetchCache{})
		if err != nil {
			t.Fatal(err)
		}
		if metadata.Status != rfs.FetchModified {
			t.Fatalf("metadata unavailable: %v", metadata.Status)
		}
		enriched, err := flow.Enrich(metadata.Page, state.Items[:1])
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("metadata title: %s", enriched[0].Title)
		return
	}
	t.Fatal("SeaDex is not registered")
}
