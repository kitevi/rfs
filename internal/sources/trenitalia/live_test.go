package trenitalia_test

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
// tests. Trenitalia publishes notices only while something is disrupting
// service, so zero applicable notices is a valid live result and is reported
// rather than failed. A page that no longer parses fails, because that is a real
// structural break.
func TestLiveTrenitalia(t *testing.T) {
	if os.Getenv("RFS_TEST_TRENITALIA_LIVE") != "1" {
		t.Skip("set RFS_TEST_TRENITALIA_LIVE=1 to probe trenitalia.com")
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
		if source.ID != "trenitalia-disruptions" {
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
		if len(state.Items) == 0 {
			t.Log("live probe: no applicable disruption is currently published for FVG; upstream coverage is unverified until one appears")
			return
		}
		t.Logf("live baseline: %d applicable notices", len(state.Items))
		for _, item := range state.Items {
			t.Logf("  %s -> %s", item.Title, item.Link)
		}
		return
	}
	t.Fatal("trenitalia-disruptions is not registered")
}
