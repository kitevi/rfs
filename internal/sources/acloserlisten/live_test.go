package acloserlisten

import (
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/kitevi/rfs/internal/rfs"
)

var (
	_ rfs.EnrichedAnnouncementFlow = Flow{}
	_ rfs.AnnouncementIdentityFlow = Flow{}
)

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
	var first rfs.ExtractedItem
	for _, item := range items {
		if strings.HasPrefix(item.GUID, guidPrefix) {
			first = item
			break
		}
	}
	if first.GUID == "" {
		t.Fatalf("no album players: %#v", items)
	}
	url, err := (Flow{}).AnnouncementEnrichmentURL(first)
	if err != nil {
		t.Fatal(err)
	}
	player, err := fetcher.Fetch(t.Context(), url, rfs.FetchCache{})
	if err != nil {
		t.Fatal(err)
	}
	item, err := (Flow{}).EnrichAnnouncement(player.Page, first)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d releases; first album: %s", len(items), item.Title)
}

// TestLiveOctoberRecommendations follows the fall revision as it stood on
// 2026-10-24: the lede names Max Richter and Nero's Day at Disneyland, and the
// page embeds a video and the Nero's Day and Caspian album players. The named
// Nero's Day album is that player, so it is announced once.
func TestLiveOctoberRecommendations(t *testing.T) {
	if os.Getenv("RFS_TEST_ACLOSERLISTEN_LIVE") == "" {
		t.Skip("set RFS_TEST_ACLOSERLISTEN_LIVE=1")
	}
	flow := Flow{}
	fetcher := rfs.NewHTTPFetcher(&http.Client{Timeout: 30 * time.Second})
	page, err := fetcher.Fetch(t.Context(), PageURL, rfs.FetchCache{})
	if err != nil {
		t.Fatal(err)
	}
	items, err := flow.Extract(page.Page)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		namedGUIDPrefix + "nerosdayatdisneyland:grandreopening": false,
		youtubeGUIDPrefix + "uZf2jkCI6Fo":                       false,
		guidPrefix + "988738690":                                false,
		guidPrefix + "4925039":                                  false,
	}
	for _, item := range items {
		if _, ok := want[item.GUID]; ok {
			want[item.GUID] = true
		}
	}
	for guid, seen := range want {
		if !seen {
			t.Skipf("live page no longer has the October revision (missing %s): %#v", guid, items)
		}
	}
	ready, pending, err := flow.PlanIdentities(nil, items)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range pending {
		url, err := flow.IdentityURL(item)
		if err != nil {
			t.Fatal(err)
		}
		player, err := fetcher.Fetch(t.Context(), url, rfs.FetchCache{})
		if err != nil {
			t.Fatal(err)
		}
		resolved, err := flow.ResolveIdentity(player.Page, item)
		if err != nil {
			t.Fatal(err)
		}
		for i := range ready {
			if ready[i].GUID == item.GUID {
				ready[i] = resolved
			}
		}
	}
	decision, err := flow.Evaluate(nil, ready)
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	for _, announcement := range decision.Announcements {
		url, err := flow.AnnouncementEnrichmentURL(announcement)
		if err != nil {
			t.Fatal(err)
		}
		if url == "" {
			titles = append(titles, announcement.Title)
			continue
		}
		player, err := fetcher.Fetch(t.Context(), url, rfs.FetchCache{})
		if err != nil {
			t.Fatal(err)
		}
		enriched, err := flow.EnrichAnnouncement(player.Page, announcement)
		if err != nil {
			t.Fatal(err)
		}
		titles = append(titles, enriched.Title)
	}
	joined := strings.Join(titles, "\n")
	for _, want := range []string{
		"Max Richter — While the Flood Rages",
		"Max Richter — Now We Can Sing (Visualiser)",
		"Nero's Day At Disneyland — Grand Reopening EP",
		"Caspian — Caspian",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("announced titles missing %q:\n%s", want, joined)
		}
	}
	if strings.Count(joined, "Grand Reopening") != 1 {
		t.Fatalf("Grand Reopening announced more than once:\n%s", joined)
	}
	_, pending, err = flow.PlanIdentities(decision.Checkpoint, items)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("named album players still pending: %#v", pending)
	}
}
