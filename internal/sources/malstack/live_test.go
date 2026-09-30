package malstack_test

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/kitevi/rfs/internal/rfs"
	"github.com/kitevi/rfs/internal/sources/malstack"
)

// TestLiveMALStack verifies the parser against the deployed page. Run it
// explicitly on the deployment host; normal tests never contact upstream.
// An access block is reported, not bypassed.
func TestLiveMALStack(t *testing.T) {
	if os.Getenv("RFS_TEST_MALSTACK_LIVE") != "1" {
		t.Skip("set RFS_TEST_MALSTACK_LIVE=1 to probe the MyAnimeList stack")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fetcher := rfs.NewHTTPFetcher(&http.Client{Timeout: 30 * time.Second})
	fetched, err := fetcher.Fetch(ctx, malstack.PageURL, rfs.FetchCache{})
	if err != nil {
		t.Fatalf("live fetch failed (report the block, do not bypass): %v", err)
	}
	if fetched.Status != rfs.FetchModified {
		t.Fatalf("live fetch returned status %d", fetched.Status)
	}
	items, err := (malstack.Flow{}).Extract(fetched.Page)
	if err != nil {
		t.Fatalf("live page no longer matches the parser: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("live page parsed to an empty collection")
	}
	t.Logf("parsed %d anime, first: %s (%s)", len(items), items[0].Title, items[0].Link)
}
