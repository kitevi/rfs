package boomkat

import (
	"os"
	"strings"
	"testing"

	"github.com/kitevi/rfs/internal/rfs"
	"github.com/kitevi/rfs/internal/rfstest"
)

// TestLiveRecordOfTheWeek is an opt-in probe of the real homepage. It polls the
// Source with rfs's own client, the one boomkat.com serves only because its
// handshake is Chrome's, and checks that the feed announces the pick:
//
//	RFS_TEST_BOOMKAT_LIVE=1 go test ./internal/sources/boomkat -run TestLive -v -count=1
func TestLiveRecordOfTheWeek(t *testing.T) {
	if os.Getenv("RFS_TEST_BOOMKAT_LIVE") == "" {
		t.Skip("set RFS_TEST_BOOMKAT_LIVE=1 to probe the live homepage")
	}
	store := rfstest.Store(t)
	source := rfs.Source{ID: "boomkat-record-of-the-week", URL: PageURL, Flow: Flow{}}
	if _, err := (rfs.Poller{Fetcher: rfs.NewHTTPFetcher(nil), Store: store}).Poll(t.Context(), source); err != nil {
		t.Fatalf("live poll: %v", err)
	}
	items := rfstest.Feed(t, store, source)
	if len(items) == 0 {
		t.Fatal("live poll announced nothing")
	}
	for _, item := range items {
		if !strings.HasPrefix(item.GUID, guidPrefix) || !strings.HasPrefix(item.Link, "https://"+siteHost+"/artists/") {
			t.Fatalf("live item = %#v", item)
		}
		t.Logf("%s: %s <%s>\n%s", flagLabel, item.Title, item.Link, item.Description)
	}
}
