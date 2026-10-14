package acloserlisten

import (
	"context"
	"encoding/json"
	"errors"
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

// trackEmbed is a Bandcamp track player, the form used when a recommendation
// is not an album embed.
func trackEmbed(trackID int64) string {
	return fmt.Sprintf(`<iframe width="100%%" src="//bandcamp.com/EmbeddedPlayer/v=2/track=%d/size=large/bgcol=333333/linkcol=ffffff/tracklist=false/artwork=small/"></iframe>`, trackID)
}

// trackPlayerPage is a track player body. albumID 0 is a standalone track.
func trackPlayerPage(t *testing.T, trackID int64, artist, title string, albumID int64, albumTitle string) rfs.Page {
	t.Helper()
	payload := map[string]any{
		"artist":      artist,
		"album_title": albumTitle,
		"tracks":      []any{map[string]any{"id": trackID, "title": title, "artist": artist}},
	}
	if albumID > 0 {
		payload["album_id"] = albumID
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return rfs.Page(`<html><body><div class="player" data-player-data="` + html.EscapeString(string(body)) + `"></div></body></html>`)
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
	if strings.Contains(items[0].Debug, "H4 ") {
		t.Fatalf("genre heading was reported as prose:\n%s", items[0].Debug)
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
		`<iframe src="//bandcamp.com/EmbeddedPlayer/v=2/album=notanumber/size=large/"></iframe>` +
		`<iframe src="//bandcamp.com/EmbeddedPlayer/v=2/track=notanumber/size=large/"></iframe>`
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

func TestEnrichAnnouncementTitlesTheYouTubeVideo(t *testing.T) {
	flow := Flow{}
	item := rfs.ExtractedItem{GUID: youtubeGUIDPrefix + "uZf2jkCI6Fo", Link: HumanURL}
	page := rfs.Page(`{"title":"Max Richter - Now We Can Sing (Visualiser)","author_name":"Max Richter"}`)
	enriched, err := flow.EnrichAnnouncement(page, item)
	if err != nil {
		t.Fatal(err)
	}
	if enriched.Title != "Max Richter — Now We Can Sing (Visualiser)" {
		t.Fatalf("title = %q", enriched.Title)
	}
	if enriched.GUID != item.GUID {
		t.Fatalf("guid = %q", enriched.GUID)
	}
	url, err := flow.AnnouncementEnrichmentURL(item)
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://www.youtube.com/oembed?format=json&url=https%3A%2F%2Fwww.youtube.com%2Fwatch%3Fv%3DuZf2jkCI6Fo" {
		t.Fatalf("enrichment URL = %q", url)
	}
}

func TestExtractKeepsTrackPlayersAndLeavesAlbumURLsAsAlbums(t *testing.T) {
	content := section("Ambient", 1) +
		trackEmbed(9) +
		`<iframe src="//bandcamp.com/EmbeddedPlayer/v=2/album=5/size=large/track=99/artwork=small/"></iframe>` +
		`<iframe class="youtube-player" src="https://www.youtube.com/embed/uZf2jkCI6Fo"></iframe>`
	items, err := (Flow{}).Extract(pageJSON(t, PageID, content))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{guidPrefix + "1", trackGUIDPrefix + "9", guidPrefix + "5", youtubeGUIDPrefix + "uZf2jkCI6Fo"}
	if len(items) != len(want) {
		t.Fatalf("got %#v, want %v", items, want)
	}
	for i, guid := range want {
		if items[i].GUID != guid {
			t.Fatalf("item %d GUID = %q, want %q", i, items[i].GUID, guid)
		}
	}
	if items[1].Description != "Genre: Ambient" {
		t.Fatalf("track description = %q, want the section it appeared under", items[1].Description)
	}
}

func TestExtractOctoberPageKeepsTheTrackAndTheAlbum(t *testing.T) {
	items, err := (Flow{}).Extract(octoberPage(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("got %#v, want the video, the track, and the album", items)
	}
	if items[0].GUID != youtubeGUIDPrefix+"uZf2jkCI6Fo" {
		t.Fatalf("video item = %#v", items[0])
	}
	if items[1].GUID != trackGUIDPrefix+"687192546" || items[1].Description != "" {
		t.Fatalf("track item = %#v", items[1])
	}
	if items[2].GUID != guidPrefix+"4925039" || items[2].Description != "" {
		t.Fatalf("album item = %#v", items[2])
	}
	trace := items[0].Debug
	for _, want := range []string{
		"kept youtube: 1",
		"skipped iframes: 0",
		"H1 kept iframe: youtube uZf2jkCI6Fo",
		"youtube.com/embed/uZf2jkCI6Fo",
		"H2 kept iframe: bandcamp track 687192546",
		"H3 kept iframe: bandcamp album 4925039",
		"H4 bold text without a genre break: Max Richter",
		"H4 bold text without a genre break: Nero",
		"H4 bold text without a genre break: Caspian",
	} {
		if !strings.Contains(trace, want) {
			t.Fatalf("trace missing %q:\n%s", want, trace)
		}
	}
}

// octoberPage is the fall revision that names three releases and embeds a
// YouTube video, a standalone track, and one album.
func octoberPage(t *testing.T) rfs.Page {
	t.Helper()
	content := `<p><strong>Max Richter</strong> returns. <strong>Nero&#8217;s Day at Disneyland</strong> offers Grand Reopening. <strong>Caspian</strong> returns.</p>` +
		`<iframe class="youtube-player" src="https://www.youtube.com/embed/uZf2jkCI6Fo?version=3&#038;rel=1"></iframe>` +
		`<iframe src="//bandcamp.com/EmbeddedPlayer/v=2/track=687192546/size=large/bgcol=333333/linkcol=ffffff/tracklist=false/artwork=small/"></iframe>` +
		`<iframe src="//bandcamp.com/EmbeddedPlayer/v=2/album=4925039/size=large/bgcol=333333/linkcol=ffffff/tracklist=false/artwork=small/"></iframe>`
	return pageJSON(t, PageID, content)
}

func TestResolveIdentityCollapsesAnAlbumTrackAndKeepsAStandaloneTrack(t *testing.T) {
	flow := Flow{}
	standalone := rfs.ExtractedItem{GUID: trackGUIDPrefix + "687192546", Link: HumanURL}
	resolved, err := flow.ResolveIdentity(trackPlayerPage(t, 687192546, "Nero's Day At Disneyland", "Ghost Debt", 0, ""), standalone)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.GUID != standalone.GUID {
		t.Fatalf("standalone GUID = %q", resolved.GUID)
	}
	if resolved.Metadata == "" {
		t.Fatal("standalone resolution did not record the track")
	}

	fromAlbum := rfs.ExtractedItem{GUID: trackGUIDPrefix + "3817274316", Description: "Genre: Rock, Post-Rock, Folk & Jazz"}
	collapsed, err := flow.ResolveIdentity(trackPlayerPage(t, 3817274316, "Caspian", "Hiraeth", 4925039, "Caspian"), fromAlbum)
	if err != nil {
		t.Fatal(err)
	}
	if collapsed.GUID != guidPrefix+"4925039" {
		t.Fatalf("album track GUID = %q, want the album", collapsed.GUID)
	}
	if collapsed.Description != fromAlbum.Description {
		t.Fatalf("collapse dropped the genre: %q", collapsed.Description)
	}
	if _, err := flow.ResolveIdentity(trackPlayerPage(t, 1, "Artist", "Song", 0, ""), standalone); err == nil {
		t.Fatal("a player for a different track was accepted")
	}
}

func TestEvaluateAnnouncesANewStandaloneTrackWithoutRepeatingAlbums(t *testing.T) {
	flow := Flow{}
	albums, err := flow.Extract(pageJSON(t, PageID, section("Ambient", 1, 2)))
	if err != nil {
		t.Fatal(err)
	}
	first, err := flow.Evaluate(nil, albums)
	if err != nil {
		t.Fatal(err)
	}
	track, err := flow.ResolveIdentity(trackPlayerPage(t, 687192546, "Nero's Day At Disneyland", "Ghost Debt", 0, ""), rfs.ExtractedItem{
		GUID: trackGUIDPrefix + "687192546", Link: HumanURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	collapsed, err := flow.ResolveIdentity(trackPlayerPage(t, 3817274316, "Caspian", "Hiraeth", 1, "Album"), rfs.ExtractedItem{
		GUID: trackGUIDPrefix + "3817274316",
	})
	if err != nil {
		t.Fatal(err)
	}
	if collapsed.GUID != guidPrefix+"1" {
		t.Fatalf("track collapsed to %q, want album 1", collapsed.GUID)
	}
	decision, err := flow.Evaluate(first.Checkpoint, []rfs.ExtractedItem{albums[0], track, collapsed})
	if err != nil {
		t.Fatal(err)
	}
	if len(decision.Announcements) != 1 || decision.Announcements[0].GUID != track.GUID {
		t.Fatalf("announced %#v, want only the standalone track", decision.Announcements)
	}
	if decision.Announcements[0].Metadata != "" {
		t.Fatalf("announcement kept identity metadata: %q", decision.Announcements[0].Metadata)
	}
	var state checkpoint
	if err := json.Unmarshal(decision.Checkpoint, &state); err != nil {
		t.Fatal(err)
	}
	if state.Tracks["687192546"] != "track:687192546" || state.Tracks["3817274316"] != "1" {
		t.Fatalf("resolved tracks = %#v", state.Tracks)
	}
	ready, pending, err := flow.PlanIdentities(decision.Checkpoint, []rfs.ExtractedItem{
		albums[0],
		{GUID: trackGUIDPrefix + "687192546"},
		{GUID: trackGUIDPrefix + "3817274316"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("known tracks still need a fetch: %#v", pending)
	}
	if len(ready) != 2 || ready[0].GUID != guidPrefix+"1" || ready[1].GUID != track.GUID {
		t.Fatalf("planned observation = %#v", ready)
	}
}

func TestPollAnnouncesStandaloneTrackAndSkipsTheResolvedPlayerNextTime(t *testing.T) {
	ctx := t.Context()
	store, err := rfs.OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	flow := Flow{}
	fetcher := &pageFetcher{pages: map[string]rfs.Page{
		PageURL: pageJSON(t, PageID, section("Ambient", 1)+section("Drone", 2)),
	}}
	for _, album := range []struct {
		id           int64
		artist, name string
	}{
		{1, "Kayla Painter", "Tectonic Particles"},
		{2, "Caspian", "Caspian"},
	} {
		item := rfs.ExtractedItem{GUID: guidPrefix + strconvI64(album.id)}
		url, err := flow.AnnouncementEnrichmentURL(item)
		if err != nil {
			t.Fatal(err)
		}
		fetcher.pages[url] = playerPage(t, album.id, album.artist, album.name)
	}
	trackItem := rfs.ExtractedItem{GUID: trackGUIDPrefix + "9"}
	albumTrack := rfs.ExtractedItem{GUID: trackGUIDPrefix + "8"}
	trackURL, err := flow.IdentityURL(trackItem)
	if err != nil {
		t.Fatal(err)
	}
	albumTrackURL, err := flow.IdentityURL(albumTrack)
	if err != nil {
		t.Fatal(err)
	}
	fetcher.pages[trackURL] = trackPlayerPage(t, 9, "Nero's Day At Disneyland", "Ghost Debt", 0, "")
	fetcher.pages[albumTrackURL] = trackPlayerPage(t, 8, "Caspian", "Hiraeth", 2, "Caspian")
	video := rfs.ExtractedItem{GUID: youtubeGUIDPrefix + "uZf2jkCI6Fo"}
	videoURL, err := flow.AnnouncementEnrichmentURL(video)
	if err != nil {
		t.Fatal(err)
	}
	fetcher.pages[videoURL] = rfs.Page(`{"title":"Max Richter - Now We Can Sing (Visualiser)","author_name":"Max Richter"}`)
	source := rfs.Source{ID: "acloserlisten", URL: PageURL, Flow: flow}
	poller := rfs.Poller{Fetcher: fetcher, Store: store}

	if _, err := poller.Poll(ctx, source); err != nil {
		t.Fatal(err)
	}
	fetcher.pages[PageURL] = pageJSON(t, PageID, `<iframe src="https://www.youtube.com/embed/uZf2jkCI6Fo"></iframe>`+embed(1)+trackEmbed(9)+trackEmbed(8))
	fetcher.fail = map[string]error{trackURL: errors.New("player down")}
	if _, err := poller.Poll(ctx, source); err == nil {
		t.Fatal("a failing track player committed anyway")
	}
	if stored := loadFeed(t, store); len(stored) != 2 {
		t.Fatalf("failed poll stored %#v", stored)
	}
	delete(fetcher.fail, trackURL)
	if _, err := poller.Poll(ctx, source); err != nil {
		t.Fatal(err)
	}
	stored := loadFeed(t, store)
	if len(stored) != 4 {
		t.Fatalf("stored %#v, want the two albums, the video, and the standalone track", stored)
	}
	if stored[youtubeGUIDPrefix+"uZf2jkCI6Fo"].Title != "Max Richter — Now We Can Sing (Visualiser)" {
		t.Fatalf("video = %#v", stored[youtubeGUIDPrefix+"uZf2jkCI6Fo"])
	}
	got := stored[trackGUIDPrefix+"9"]
	if got.Title != "Nero's Day At Disneyland — Ghost Debt" || got.Metadata != "" {
		t.Fatalf("standalone track = %#v", got)
	}
	if _, ok := stored[trackGUIDPrefix+"8"]; ok {
		t.Fatal("a track from an announced album was published again")
	}
	raw, err := store.LoadAnnouncementCheckpoint(ctx, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	var traced checkpoint
	if err := json.Unmarshal(raw, &traced); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"H1 kept iframe: youtube uZf2jkCI6Fo",
		"youtube.com",
		"H2 resolved track 9: album_id=none",
		`track="Ghost Debt"`,
		"H2 resolved track 8: album_id=2",
		"H5 new: " + youtubeGUIDPrefix + "uZf2jkCI6Fo " + trackGUIDPrefix + "9",
	} {
		if !strings.Contains(traced.Trace, want) {
			t.Fatalf("trace missing %q:\n%s", want, traced.Trace)
		}
	}
	hits := map[string]int{}
	for url, count := range fetcher.hits {
		hits[url] = count
	}
	if _, err := poller.Poll(ctx, source); err != nil {
		t.Fatal(err)
	}
	if fetcher.hits[trackURL] != hits[trackURL] || fetcher.hits[albumTrackURL] != hits[albumTrackURL] {
		t.Fatalf("resolved players were fetched again: before %v after %v", hits, fetcher.hits)
	}
	if fetcher.hits[videoURL] != hits[videoURL] {
		t.Fatalf("youtube metadata was fetched again: before %v after %v", hits, fetcher.hits)
	}
	if len(loadFeed(t, store)) != 4 {
		t.Fatal("repeat poll changed the feed")
	}
}

func strconvI64(id int64) string {
	return fmt.Sprintf("%d", id)
}

func loadFeed(t *testing.T, store *rfs.SQLiteStore) map[string]rfs.Item {
	t.Helper()
	items, err := store.LoadSnapshot(context.Background(), "acloserlisten")
	if err != nil {
		t.Fatal(err)
	}
	stored := make(map[string]rfs.Item, len(items))
	for _, item := range items {
		stored[item.GUID] = item
	}
	return stored
}

type pageFetcher struct {
	pages map[string]rfs.Page
	hits  map[string]int
	fail  map[string]error
}

func (f *pageFetcher) Fetch(_ context.Context, url string, _ rfs.FetchCache) (rfs.FetchResult, error) {
	if f.hits == nil {
		f.hits = map[string]int{}
	}
	f.hits[url]++
	if err := f.fail[url]; err != nil {
		return rfs.FetchResult{}, err
	}
	page, ok := f.pages[url]
	if !ok {
		return rfs.FetchResult{}, fmt.Errorf("unexpected fetch %s", url)
	}
	return rfs.FetchResult{Status: rfs.FetchModified, Page: page}, nil
}
