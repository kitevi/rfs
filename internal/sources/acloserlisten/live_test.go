package acloserlisten

import (
	"encoding/json"
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
	t.Logf("%d releases; first: %s", len(items), item.Title)
}

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
	var albums []string
	seenTrack := false
	seenAlbum := false
	seenVideo := false
	for _, item := range items {
		switch item.GUID {
		case trackGUIDPrefix + "687192546":
			seenTrack = true
		case guidPrefix + "4925039":
			seenAlbum = true
			albums = append(albums, "4925039")
		case youtubeGUIDPrefix + "uZf2jkCI6Fo":
			seenVideo = true
		default:
			if strings.HasPrefix(item.GUID, guidPrefix) {
				albums = append(albums, strings.TrimPrefix(item.GUID, guidPrefix))
			}
		}
	}
	if !seenTrack || !seenAlbum || !seenVideo {
		t.Fatalf("live page no longer has the October video, track, and album: %#v", items)
	}
	checkpoint, err := json.Marshal(map[string]any{"version": CheckpointVersion, "announced": albums})
	if err != nil {
		t.Fatal(err)
	}
	ready, pending, err := flow.PlanIdentities(checkpoint, items)
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
	decision, err := flow.Evaluate(checkpoint, ready)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{youtubeGUIDPrefix + "uZf2jkCI6Fo", trackGUIDPrefix + "687192546"}
	if len(decision.Announcements) != len(want) {
		t.Fatalf("announced %#v, want the video and the standalone track", decision.Announcements)
	}
	for i, guid := range want {
		if decision.Announcements[i].GUID != guid {
			t.Fatalf("announcement %d = %q, want %q", i, decision.Announcements[i].GUID, guid)
		}
		url, err := flow.AnnouncementEnrichmentURL(decision.Announcements[i])
		if err != nil {
			t.Fatal(err)
		}
		player, err := fetcher.Fetch(t.Context(), url, rfs.FetchCache{})
		if err != nil {
			t.Fatal(err)
		}
		enriched, err := flow.EnrichAnnouncement(player.Page, decision.Announcements[i])
		if err != nil {
			t.Fatal(err)
		}
		switch guid {
		case youtubeGUIDPrefix + "uZf2jkCI6Fo":
			if enriched.Title != "Max Richter — Now We Can Sing (Visualiser)" {
				t.Fatalf("video title = %q", enriched.Title)
			}
		default:
			if enriched.Title != "Nero's Day At Disneyland — Ghost Debt" {
				t.Fatalf("track title = %q", enriched.Title)
			}
		}
	}
	_, pending, err = flow.PlanIdentities(decision.Checkpoint, items)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("resolved track still pending: %#v", pending)
	}
}
