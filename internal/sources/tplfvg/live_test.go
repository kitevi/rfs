package tplfvg_test

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/ppowo/rfs/internal/rfs"
	"github.com/ppowo/rfs/internal/sources"
)

// Run explicitly on the deployment host; never contacts upstream in normal
// tests. This is the discovery-gate probe: it proves the live collection lists
// notices and that each in-force notice's detail page parses on this host.
func TestLiveTPLFVG(t *testing.T) {
	if os.Getenv("RFS_TEST_TPL_FVG_LIVE") != "1" {
		t.Skip("set RFS_TEST_TPL_FVG_LIVE=1 to probe tplfvg.it")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	store, err := rfs.OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	poller := rfs.Poller{Fetcher: rfs.NewHTTPFetcher(&http.Client{Timeout: 30 * time.Second}), Store: store}
	for _, source := range sources.All() {
		if source.ID != "tpl-fvg-scioperi" {
			continue
		}
		result, err := poller.Poll(ctx, source)
		if err != nil {
			t.Fatalf("poll: %v", err)
		}
		if result.Status == rfs.PollThrottled {
			t.Fatalf("upstream throttled: retry after %s", result.RetryAfter)
		}
		state, err := store.LoadChangeState(ctx, source.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !state.Initialized {
			t.Fatal("poll did not establish a baseline")
		}
		t.Logf("live baseline: %d notices in force", len(state.Items))
		for _, item := range state.Items {
			t.Logf("  %s -> %s", item.Title, item.Link)
		}
		items, err := store.LoadSnapshot(ctx, source.ID)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("first-run feed items: %d", len(items))
		return
	}
	t.Fatal("tpl-fvg-scioperi is not registered")
}
