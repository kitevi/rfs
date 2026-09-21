package onepiece

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveOnePieceArchive is an opt-in probe of the real archive. It verifies
// that ordinary HTTP access works from this host and that the parser still
// matches the deployed markup, without reading chapter content:
//
//	RFS_TEST_ONEPIECE_LIVE=1 go test ./internal/sources/onepiece -run TestLive -v -count=1
func TestLiveOnePieceArchive(t *testing.T) {
	if os.Getenv("RFS_TEST_ONEPIECE_LIVE") == "" {
		t.Skip("set RFS_TEST_ONEPIECE_LIVE=1 to probe the live archive")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(PageURL)
	if err != nil {
		t.Fatalf("fetch %s: %v", PageURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fetch %s: status %s", PageURL, resp.Status)
	}
	page, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		t.Fatalf("read page: %v", err)
	}
	items, err := Flow{}.Extract(page)
	if err != nil {
		t.Fatalf("extract live archive: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("live archive returned no chapters")
	}
	for _, item := range items {
		if !strings.HasPrefix(item.GUID, guidPrefix) {
			t.Fatalf("live item GUID = %q", item.GUID)
		}
		if !strings.HasPrefix(item.Link, "https://"+siteHost+"/chapters/") {
			t.Fatalf("live item link = %q", item.Link)
		}
	}
	t.Logf("live archive: %d chapters, newest %s", len(items), items[0].GUID)
}
