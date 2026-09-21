package acloserlisten

import (
	"encoding/json"
	"fmt"
	"html"
	"strings"
	"testing"

	"github.com/kitevi/rfs/internal/rfs"
)

// pageJSON builds the WordPress response body the Flow consumes.
func pageJSON(t *testing.T, id int, content string) rfs.Page {
	t.Helper()
	body, err := json.Marshal(map[string]any{"ID": id, "content": content})
	if err != nil {
		t.Fatal(err)
	}
	return rfs.Page(body)
}

// embed is the Bandcamp iframe markup the recommendations page carries.
func embed(albumID int64) string {
	return fmt.Sprintf(`<iframe width="100%%" src="//bandcamp.com/EmbeddedPlayer/v=2/album=%d/size=large/bgcol=333333/linkcol=2ebd35/tracklist=false/artwork=small/"></iframe>`, albumID)
}

// section is a genre lead-in followed by that genre's embeds.
func section(genre string, albumIDs ...int64) string {
	var b strings.Builder
	b.WriteString("<p><strong>" + genre + "<br /></strong>Prose about the releases.</p>")
	for _, id := range albumIDs {
		b.WriteString(embed(id))
	}
	return b.String()
}

// playerPage is the Bandcamp EmbeddedPlayer body carrying data-player-data.
func playerPage(t *testing.T, albumID int64, artist, title string) rfs.Page {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"album_id": albumID, "artist": artist, "album_title": title, "tracks": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	return rfs.Page(`<html><body><div class="player" data-player-data="` + html.EscapeString(string(payload)) + `"></div></body></html>`)
}

func TestExtractAnnouncesEmbeddedAlbumsInDocumentOrder(t *testing.T) {
	content := "<p>Intro prose.</p>" + section("Ambient", 1032673665, 3804917300) + section("Drone", 2813179140)
	items, err := (Flow{}).Extract(pageJSON(t, PageID, content))
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		guid        string
		description string
	}{
		{"acloserlisten:bandcamp:album:1032673665", "Genre: Ambient"},
		{"acloserlisten:bandcamp:album:3804917300", "Genre: Ambient"},
		{"acloserlisten:bandcamp:album:2813179140", "Genre: Drone"},
	}
	if len(items) != len(want) {
		t.Fatalf("got %d items, want %d: %#v", len(items), len(want), items)
	}
	for i, expected := range want {
		if items[i].GUID != expected.guid {
			t.Fatalf("item %d GUID = %q, want %q", i, items[i].GUID, expected.guid)
		}
		if items[i].Link != HumanURL {
			t.Fatalf("item %d link = %q, want %q", i, items[i].Link, HumanURL)
		}
		if items[i].Description != expected.description {
			t.Fatalf("item %d description = %q, want %q", i, items[i].Description, expected.description)
		}
	}
}

func TestExtractDeduplicatesRepeatedEmbeds(t *testing.T) {
	content := section("Ambient", 1032673665, 1032673665) + section("Drone", 1032673665)
	items, err := (Flow{}).Extract(pageJSON(t, PageID, content))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1: %#v", len(items), items)
	}
	if items[0].Description != "Genre: Ambient" {
		t.Fatalf("duplicate kept the later genre: %q", items[0].Description)
	}
}

func TestExtractAcceptsEquivalentBandcampEmbeds(t *testing.T) {
	content := section("Ambient", 1) + `<iframe src="https://www.bandcamp.com/EmbeddedPlayer/v=2/album=2/size=small/artwork=small/"></iframe>`
	items, err := (Flow{}).Extract(pageJSON(t, PageID, content))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2: %#v", len(items), items)
	}
}

func TestExtractIgnoresForeignFrames(t *testing.T) {
	content := section("Ambient", 1) +
		`<iframe src="https://www.youtube.com/embed?v=album=5"></iframe>` +
		`<iframe src="https://evil.example/EmbeddedPlayer/v=2/album=6/size=large/"></iframe>` +
		`<iframe src="//bandcamp.com/EmbeddedPlayer/v=2/album=notanumber/size=large/"></iframe>`
	items, err := (Flow{}).Extract(pageJSON(t, PageID, content))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].GUID != "acloserlisten:bandcamp:album:1" {
		t.Fatalf("foreign frames contributed items: %#v", items)
	}
}

func TestExtractKeepsAlbumsWithoutAGenreLabel(t *testing.T) {
	content := embed(1032673665) + section("Ambient", 3804917300)
	items, err := (Flow{}).Extract(pageJSON(t, PageID, content))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
	if items[0].Description != "" {
		t.Fatalf("genre-less album description = %q, want empty", items[0].Description)
	}
}

func TestExtractRejectsUnusablePages(t *testing.T) {
	cases := map[string]rfs.Page{
		"not JSON":            rfs.Page("<html></html>"),
		"unrelated page":      pageJSON(t, 999, section("Ambient", 1)),
		"no content":          rfs.Page(`{"ID":395}`),
		"no embedded albums":  pageJSON(t, PageID, "<p>Nothing here.</p>"),
		"only foreign frames": pageJSON(t, PageID, `<iframe src="https://example.com/EmbeddedPlayer/v=2/album=1/"></iframe>`),
	}
	for name, page := range cases {
		if _, err := (Flow{}).Extract(page); err == nil {
			t.Fatalf("%s: Extract succeeded, want an error", name)
		}
	}
}

func TestEvaluateAnnouncesEachAlbumOnce(t *testing.T) {
	flow := Flow{}
	extract := func(content string) []rfs.ExtractedItem {
		t.Helper()
		items, err := flow.Extract(pageJSON(t, PageID, content))
		if err != nil {
			t.Fatal(err)
		}
		return items
	}

	first := extract(section("Ambient", 1, 2))
	decision, err := flow.Evaluate(nil, first)
	if err != nil {
		t.Fatal(err)
	}
	if len(decision.Announcements) != 2 {
		t.Fatalf("first observation announced %d items, want 2", len(decision.Announcements))
	}
	if len(decision.Checkpoint) == 0 {
		t.Fatal("first observation returned an empty checkpoint")
	}

	repeated, err := flow.Evaluate(decision.Checkpoint, first)
	if err != nil {
		t.Fatal(err)
	}
	if len(repeated.Announcements) != 0 {
		t.Fatalf("unchanged observation announced %#v", repeated.Announcements)
	}

	// A whole-page replacement announces only the newly embedded albums.
	replaced := extract(section("Drone", 3, 4))
	decision, err = flow.Evaluate(repeated.Checkpoint, replaced)
	if err != nil {
		t.Fatal(err)
	}
	if len(decision.Announcements) != 2 || decision.Announcements[0].GUID != guidPrefix+"3" {
		t.Fatalf("replacement announced %#v", decision.Announcements)
	}

	// An album that leaves the page and later returns is not announced twice.
	gapped, err := flow.Evaluate(decision.Checkpoint, extract(section("Drone", 4)))
	if err != nil {
		t.Fatal(err)
	}
	if len(gapped.Announcements) != 0 {
		t.Fatalf("dropping an album announced %#v", gapped.Announcements)
	}
	returned, err := flow.Evaluate(gapped.Checkpoint, extract(section("Drone", 3, 4)))
	if err != nil {
		t.Fatal(err)
	}
	if len(returned.Announcements) != 0 {
		t.Fatalf("a returning album was announced again: %#v", returned.Announcements)
	}

	// Reordering keeps the checkpoint and announces nothing.
	reordered, err := flow.Evaluate(returned.Checkpoint, extract(section("Drone", 4, 3)))
	if err != nil {
		t.Fatal(err)
	}
	if len(reordered.Announcements) != 0 {
		t.Fatalf("reordering announced %#v", reordered.Announcements)
	}
}

func TestEvaluateAlwaysReturnsAUsableCheckpoint(t *testing.T) {
	flow := Flow{}
	items, err := flow.Extract(pageJSON(t, PageID, section("Ambient", 1)))
	if err != nil {
		t.Fatal(err)
	}
	first, err := flow.Evaluate(nil, items)
	if err != nil {
		t.Fatal(err)
	}
	second, err := flow.Evaluate(first.Checkpoint, items)
	if err != nil {
		t.Fatal(err)
	}
	var state struct {
		Version   int      `json:"version"`
		Announced []string `json:"announced"`
	}
	if err := json.Unmarshal(second.Checkpoint, &state); err != nil {
		t.Fatal(err)
	}
	if state.Version != CheckpointVersion || len(state.Announced) != 1 || state.Announced[0] != "1" {
		t.Fatalf("checkpoint = %s", second.Checkpoint)
	}
}

func TestEvaluateRejectsUnusableObservationsAndCheckpoints(t *testing.T) {
	flow := Flow{}
	observed, err := flow.Extract(pageJSON(t, PageID, section("Ambient", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := flow.Evaluate(nil, nil); err == nil {
		t.Fatal("empty observation was accepted")
	}
	if _, err := flow.Evaluate(nil, []rfs.ExtractedItem{{GUID: "one-piece:chapter:1080"}}); err == nil {
		t.Fatal("foreign GUID was accepted")
	}
	if _, err := flow.Evaluate(json.RawMessage("{"), observed); err == nil {
		t.Fatal("invalid checkpoint JSON was accepted")
	}
	if _, err := flow.Evaluate(json.RawMessage(`{"version":99,"announced":["1"]}`), observed); err == nil {
		t.Fatal("unsupported checkpoint version was accepted")
	}
}

func TestAnnouncementEnrichmentURLNamesTheAlbumPlayer(t *testing.T) {
	flow := Flow{}
	url, err := flow.AnnouncementEnrichmentURL(rfs.ExtractedItem{GUID: guidPrefix + "1032673665"})
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://bandcamp.com/EmbeddedPlayer/v=2/album=1032673665/size=large/artwork=small/" {
		t.Fatalf("enrichment URL = %q", url)
	}
	if _, err := flow.AnnouncementEnrichmentURL(rfs.ExtractedItem{GUID: "one-piece:chapter:1080"}); err == nil {
		t.Fatal("foreign GUID produced an enrichment URL")
	}
}

func TestEnrichAnnouncementTitlesTheAlbum(t *testing.T) {
	flow := Flow{}
	item := rfs.ExtractedItem{
		GUID:        guidPrefix + "1032673665",
		Link:        HumanURL,
		Description: "Genre: Ambient",
	}
	enriched, err := flow.EnrichAnnouncement(playerPage(t, 1032673665, "Mary Lattimore & Julianna Barwick", "Tragic Magic"), item)
	if err != nil {
		t.Fatal(err)
	}
	if enriched.Title != "Mary Lattimore & Julianna Barwick — Tragic Magic" {
		t.Fatalf("title = %q", enriched.Title)
	}
	if enriched.GUID != item.GUID || enriched.Link != item.Link || enriched.Description != item.Description {
		t.Fatalf("enrichment changed more than the title: %+v", enriched)
	}
}

func TestEnrichAnnouncementKeepsAmpersandsLiteral(t *testing.T) {
	flow := Flow{}
	item := rfs.ExtractedItem{GUID: guidPrefix + "7"}
	enriched, err := flow.EnrichAnnouncement(playerPage(t, 7, "Drum & Lace", "Terra"), item)
	if err != nil {
		t.Fatal(err)
	}
	if enriched.Title != "Drum & Lace — Terra" {
		t.Fatalf("title = %q", enriched.Title)
	}
}

func TestEnrichAnnouncementRejectsUnusablePlayers(t *testing.T) {
	flow := Flow{}
	item := rfs.ExtractedItem{GUID: guidPrefix + "7"}
	cases := map[string]struct {
		page rfs.Page
		item rfs.ExtractedItem
	}{
		"no player data":  {rfs.Page(`<html><body><div class="player"></div></body></html>`), item},
		"invalid JSON":    {rfs.Page(`<html><body><div data-player-data="{}"></div></body></html>`), item},
		"different album": {playerPage(t, 8, "Artist", "Album"), item},
		"missing artist":  {playerPage(t, 7, "", "Album"), item},
		"missing title":   {playerPage(t, 7, "Artist", "   "), item},
		"unexpected GUID": {playerPage(t, 7, "Artist", "Album"), rfs.ExtractedItem{GUID: "one-piece:chapter:1080"}},
	}
	for name, candidate := range cases {
		if _, err := flow.EnrichAnnouncement(candidate.page, candidate.item); err == nil {
			t.Fatalf("%s: EnrichAnnouncement succeeded, want an error", name)
		}
	}
}
