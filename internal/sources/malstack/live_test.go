package malstack_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/kitevi/rfs/internal/rfs"
	"github.com/kitevi/rfs/internal/sources/malstack"
)

// TestLiveMALStack verifies the parser against both deployed stack pages. Run it
// explicitly on the deployment host; normal tests never contact upstream. It
// fetches with rfs's own client, so a block that client cannot pass fails the
// probe instead of being worked around here.
func TestLiveMALStack(t *testing.T) {
	if os.Getenv("RFS_TEST_MALSTACK_LIVE") != "1" {
		t.Skip("set RFS_TEST_MALSTACK_LIVE=1 to probe the MyAnimeList stack")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	fetcher := rfs.NewHTTPFetcher(nil)
	for _, stackID := range []string{malstack.NewAnimeIPs, malstack.NextSakuga} {
		fetched, err := fetcher.Fetch(ctx, malstack.PageURL(stackID), rfs.FetchCache{})
		if err != nil {
			t.Fatalf("stack %s: live fetch failed: %v", stackID, err)
		}
		if fetched.Status != rfs.FetchModified {
			t.Fatalf("stack %s: live fetch returned status %d", stackID, fetched.Status)
		}
		items, err := (malstack.Flow{StackID: stackID}).Extract(fetched.Page)
		if err != nil {
			t.Fatalf("stack %s: live page no longer matches the parser: %v", stackID, err)
		}
		if len(items) == 0 {
			t.Fatalf("stack %s: live page parsed to an empty collection", stackID)
		}
		t.Logf("stack %s: parsed %d anime, first: %s (%s)", stackID, len(items), items[0].Title, items[0].Link)
	}
}
