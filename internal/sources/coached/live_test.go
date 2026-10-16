package coached

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveEssayIndex is an opt-in probe of the real essay library index. It
// verifies that ordinary HTTP access works from this host and that the parser
// still matches the deployed markup:
//
//	RFS_TEST_COACHED_LIVE=1 go test ./internal/sources/coached -run TestLive -v -count=1
func TestLiveEssayIndex(t *testing.T) {
	if os.Getenv("RFS_TEST_COACHED_LIVE") == "" {
		t.Skip("set RFS_TEST_COACHED_LIVE=1 to probe the live index")
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
		t.Fatalf("extract live index: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("live index returned no essays")
	}
	for _, item := range items {
		if !strings.HasPrefix(item.GUID, guidPrefix) {
			t.Fatalf("live item GUID = %q", item.GUID)
		}
		if !strings.HasPrefix(item.Link, "https://"+siteHost+"/essays/") || strings.Contains(item.Link, "/essays/issues/") {
			t.Fatalf("live item link = %q", item.Link)
		}
		if item.PubDate != nil {
			t.Fatalf("live item %s has a pubDate", item.GUID)
		}
		if strings.TrimSpace(item.Title) == "" {
			t.Fatalf("live item %s has an empty title", item.GUID)
		}
	}
	t.Logf("live index: %d essays, first %s (%s)", len(items), items[0].GUID, items[0].Title)
}
