package cittafiera

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kitevi/rfs/internal/rfs"
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
	items, err := Flow{}.Extract(fetchPage(t, PageURL))
	if err != nil {
		t.Fatalf("extract live homepage: %v", err)
	}
	flow := Flow{}
	item := items[0]
	if !strings.HasPrefix(item.GUID, guidPrefix) || item.Link != filmURL(strings.TrimPrefix(item.GUID, guidPrefix)) {
		t.Fatalf("live item = %#v", item)
	}
	resolved, err := flow.ResolveIdentity(fetchPage(t, item.Link), item)
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

func fetchPage(t *testing.T, pageURL string) rfs.Page {
	t.Helper()
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(pageURL)
	if err != nil {
		t.Fatalf("fetch %s: %v", pageURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fetch %s: status %s", pageURL, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		t.Fatalf("read %s: %v", pageURL, err)
	}
	return body
}
