package cittafiera

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kitevi/rfs/internal/rfstest"
)

// TestLiveHomepage is an opt-in probe of the real homepage. It verifies that
// ordinary HTTP access works from this host and that one film page still
// lists a screening day:
//
//	RFS_TEST_CITTAFIERA_LIVE=1 go test ./internal/sources/cittafiera -run TestLive -v -count=1
func TestLiveHomepage(t *testing.T) {
	if os.Getenv("RFS_TEST_CITTAFIERA_LIVE") == "" {
		t.Skip("set RFS_TEST_CITTAFIERA_LIVE=1 to probe the live homepage")
	}
	items, err := Flow{}.Extract(rfstest.FetchLive(t, PageURL))
	if err != nil {
		t.Fatalf("extract live homepage: %v", err)
	}
	flow := Flow{}
	item := items[0]
	if !strings.HasPrefix(item.GUID, guidPrefix) || item.Link != filmURL(strings.TrimPrefix(item.GUID, guidPrefix)) {
		t.Fatalf("live item = %#v", item)
	}
	resolved, err := flow.ResolveIdentity(rfstest.FetchLive(t, item.Link), item)
	if err != nil {
		t.Fatalf("classify %s: %v", item.Link, err)
	}
	var note filmNote
	if err := json.Unmarshal([]byte(resolved.Metadata), &note); err != nil {
		t.Fatalf("decode note: %v", err)
	}
	if _, err := time.Parse("2006-01-02", note.First); err != nil {
		t.Fatalf("film %s first day = %q", item.Link, note.First)
	}
	t.Logf("live homepage: %d films, first %s on %s", len(items), item.Title, note.First)
}
