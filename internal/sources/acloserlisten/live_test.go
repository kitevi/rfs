package acloserlisten

import (
	"github.com/kitevi/rfs/internal/rfs"
	"net/http"
	"os"
	"testing"
	"time"
)

var _ rfs.EnrichedAnnouncementFlow = Flow{}

func TestLiveRecommendations(t *testing.T) {
	if os.Getenv("RFS_TEST_ACLOSERLISTEN_LIVE") == "" {
		t.Skip("set RFS_TEST_ACLOSERLISTEN_LIVE=1")
	}
	fetcher := rfs.NewHTTPFetcher(&http.Client{Timeout: 30 * time.Second})
	page, err := fetcher.Fetch(t.Context(), PageURL, rfs.FetchCache{})
	if err != nil {
		t.Fatal(err)
	}
	items, err := (Flow{}).Extract(page.Page)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("no albums")
	}
	url, err := (Flow{}).AnnouncementEnrichmentURL(items[0])
	if err != nil {
		t.Fatal(err)
	}
	player, err := fetcher.Fetch(t.Context(), url, rfs.FetchCache{})
	if err != nil {
		t.Fatal(err)
	}
	item, err := (Flow{}).EnrichAnnouncement(player.Page, items[0])
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d albums; first: %s", len(items), item.Title)
}
