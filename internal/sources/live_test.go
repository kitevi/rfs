package sources_test

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/ppowo/rfs/internal/rfs"
	"github.com/ppowo/rfs/internal/sources"
)

// TestLiveTransportFeeds polls every transport Source against the live upstream
// and reports what each decoded. Run it explicitly on the deployment host; the
// normal test run never contacts upstream. A Source that fails its poll fails
// the probe, because that is a real structural break, while a legitimate empty
// list is reported rather than hidden.
func TestLiveTransportFeeds(t *testing.T) {
	if os.Getenv("RFS_TEST_TRANSPORT_LIVE") != "1" {
		t.Skip("set RFS_TEST_TRANSPORT_LIVE=1 to probe the operator sites")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	store, err := rfs.OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	poller := rfs.Poller{Fetcher: rfs.NewHTTPFetcher(&http.Client{Timeout: 45 * time.Second}), Store: store}
	watched := map[string]bool{
		"trenitalia-disruptions": true,
		"arriva-udine":           true,
		"trieste-trasporti":      true,
		"apt-gorizia":            true,
	}
	seen := map[string]bool{}
	for _, source := range sources.All() {
		if !watched[source.ID] {
			continue
		}
		seen[source.ID] = true
		if _, err := poller.Poll(ctx, source); err != nil {
			t.Errorf("%s: %v", source.ID, err)
			continue
		}
		state, err := store.LoadChangeState(ctx, source.ID)
		if err != nil {
			t.Errorf("%s: %v", source.ID, err)
			continue
		}
		if !state.Initialized {
			t.Errorf("%s: the poll established no baseline", source.ID)
			continue
		}
		t.Logf("%s: %d notices in force", source.ID, len(state.Items))
		for _, item := range state.Items {
			t.Logf("  %s", item.Title)
		}
	}
	for id := range watched {
		if !seen[id] {
			t.Fatalf("%s is not registered", id)
		}
	}
}
