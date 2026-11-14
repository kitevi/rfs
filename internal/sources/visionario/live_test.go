package visionario

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/kitevi/rfs/internal/rfstest"
)

// TestLiveArchive is an opt-in probe of the real film archive. It verifies
// that ordinary HTTP access works from this host and that the parser still
// matches the deployed markup. One release page must carry an opening date:
//
//	RFS_TEST_VISIONARIO_LIVE=1 go test ./internal/sources/visionario -run TestLive -v -count=1
func TestLiveArchive(t *testing.T) {
	if os.Getenv("RFS_TEST_VISIONARIO_LIVE") == "" {
		t.Skip("set RFS_TEST_VISIONARIO_LIVE=1 to probe the live archive")
	}
	items, err := Flow{}.Extract(rfstest.FetchLive(t, PageURL))
	if err != nil {
		t.Fatalf("extract live archive: %v", err)
	}
	flow := Flow{}
	var releaseTitle string
	for _, item := range items {
		if !strings.HasPrefix(item.GUID, guidPrefix) || item.Link != filmURL(strings.TrimPrefix(item.GUID, guidPrefix)) {
			t.Fatalf("live item = %#v", item)
		}
		resolved, err := flow.ResolveIdentity(rfstest.FetchLive(t, item.Link), item)
		if err != nil {
			t.Fatalf("classify %s: %v", item.Link, err)
		}
		var note filmNote
		if err := json.Unmarshal([]byte(resolved.Metadata), &note); err != nil {
			t.Fatalf("decode note for %s: %v", item.Link, err)
		}
		if note.Kind != kindRelease {
			continue
		}
		if !openingLine.MatchString(note.Opening) {
			t.Fatalf("release %s opening = %q", item.Link, note.Opening)
		}
		releaseTitle = item.Title
		break
	}
	if releaseTitle == "" {
		t.Fatal("live archive had no release with an opening date")
	}
	t.Logf("live archive: %d films, release %s", len(items), releaseTitle)
}
