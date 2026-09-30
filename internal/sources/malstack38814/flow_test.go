package malstack38814_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/kitevi/rfs/internal/rfs"
	"github.com/kitevi/rfs/internal/sources/malstack38814"
)

const stackPage = `<!doctype html>
<html><head>
<meta property="og:url" content="https://myanimelist.net/stacks/38814">
<meta property="og:description" content="MyAnimeList - Interest Stacks - 1 Entries, 270 Restacks">
</head><body>
<div class="content-left stacks-detail">
  <h2 class="title">The Next Sakuga Shows</h2>
  <div class="tag"><span class="tag-anime">Anime</span></div>
  <div class="list-anime-list">
    <div class="seasonal-anime js-seasonal-anime">
      <div class="head"><div class="title-text">
        <h2 class="h2_anime_title"><a class="link-title" href="https://myanimelist.net/anime/59878/Agami">Agami</a></h2>
      </div></div>
      <div class="info">Movie, 2026, 1 ep</div>
      <div class="intro">First paragraph.<br><br>Second &amp; final.</div>
    </div>
  </div>
</div>
<div class="content-right">
  <div class="seasonal-anime"><a class="link-title" href="https://myanimelist.net/anime/21/One_Piece">One Piece</a></div>
</div>
</body></html>`

type testEntry struct {
	id    string
	title string
	notes string
}

func stackHTML(count int, entries ...testEntry) string {
	cards := ""
	for _, e := range entries {
		cards += `<div class="seasonal-anime js-seasonal-anime">
      <div class="head"><div class="title-text">
        <h2 class="h2_anime_title"><a class="link-title" href="https://myanimelist.net/anime/` + e.id + `/` + e.title + `">` + e.title + `</a></h2>
      </div></div>
      <div class="info">TV, 2026, ? eps</div>
      <div class="intro">` + e.notes + `</div>
    </div>`
	}
	return `<!doctype html>
<html><head>
<meta property="og:url" content="https://myanimelist.net/stacks/38814">
<meta property="og:description" content="MyAnimeList - Interest Stacks - ` + fmt.Sprint(count) + ` Entries, 270 Restacks">
</head><body>
<div class="content-left stacks-detail">
  <div class="list-anime-list">` + cards + `</div>
</div>
</body></html>`
}

func observations(t *testing.T, page string) []rfs.ExtractedItem {
	t.Helper()
	items, err := (malstack38814.Flow{}).Extract(rfs.Page(page))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return items
}

func TestChangesReportsAdditionsRemovalsAndNoteEdits(t *testing.T) {
	before := observations(t, stackHTML(3,
		testEntry{id: "59878", title: "Alpha", notes: "Old note<br>Tail"},
		testEntry{id: "60000", title: "Beta", notes: "Beta note"},
		testEntry{id: "59999", title: "Kept", notes: "Unchanged"},
	))
	after := observations(t, stackHTML(3,
		testEntry{id: "59878", title: "Alpha", notes: "New note<br>Tail"},
		testEntry{id: "60001", title: "Gamma", notes: ""},
		testEntry{id: "59999", title: "Kept", notes: "Unchanged"},
	))
	changes, err := (malstack38814.Flow{}).Changes(before, after)
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if len(changes) != 3 {
		t.Fatalf("expected 3 changes, got %d: %#v", len(changes), changes)
	}
	wantTitles := []string{"Notes updated: Alpha", "Removed: Beta", "Added: Gamma"}
	wantGUIDs := []string{"59878", "60000", "60001"}
	for i := range changes {
		if changes[i].Title != wantTitles[i] || changes[i].GUID != wantGUIDs[i] {
			t.Fatalf("change %d = %q (%s), want %q (%s)", i, changes[i].Title, changes[i].GUID, wantTitles[i], wantGUIDs[i])
		}
	}
	edit := changes[0]
	if !strings.Contains(edit.Description, "- Old note") || !strings.Contains(edit.Description, "+ New note") {
		t.Fatalf("note diff missing changed span: %q", edit.Description)
	}
	if strings.Contains(edit.Description, "Tail") {
		t.Fatalf("common trailing lines must be trimmed from the diff: %q", edit.Description)
	}
	removed := changes[1]
	if !strings.Contains(removed.Description, "Beta note") {
		t.Fatalf("removal must keep the last observed notes: %q", removed.Description)
	}
	added := changes[2]
	if !strings.Contains(added.Description, "Added to the stack") {
		t.Fatalf("addition must carry a membership notice: %q", added.Description)
	}
	if len(added.Description) != 0 && strings.Contains(added.Description, "<script") {
		t.Fatalf("addition leaked markup: %q", added.Description)
	}
}

func TestChangesTreatsRecognizedEmptyCollectionAsRemovals(t *testing.T) {
	before := observations(t, stackHTML(1, testEntry{id: "59878", title: "Alpha", notes: "Alpha note"}))
	after, err := (malstack38814.Flow{}).Extract(rfs.Page(stackHTML(0)))
	if err != nil {
		t.Fatalf("an explicit zero-entry collection must be recognized: %v", err)
	}
	if len(after) != 0 {
		t.Fatalf("expected no anime, got %d", len(after))
	}
	changes, err := (malstack38814.Flow{}).Changes(before, after)
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if len(changes) != 1 || changes[0].Title != "Removed: Alpha" {
		t.Fatalf("expected one removal, got %#v", changes)
	}
}

func TestChangesIgnoresUnchangedObservationsAndPresentationNoise(t *testing.T) {
	before := observations(t, stackHTML(2,
		testEntry{id: "59878", title: "Alpha", notes: "Same note"},
		testEntry{id: "60000", title: "Beta", notes: "Beta note"},
	))
	after := observations(t, stackHTML(2,
		testEntry{id: "60000", title: "Beta renamed", notes: "Beta note"},
		testEntry{id: "59878", title: "Alpha", notes: "Same&nbsp;  note"},
	))
	changes, err := (malstack38814.Flow{}).Changes(before, after)
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("reordering or title/whitespace presentation noise produced changes: %#v", changes)
	}
}

func TestFlowStoresCanonicalNoteLines(t *testing.T) {
	notes := `Line one.<br><br>  Line   two &amp; more. <a href="https://example.com/x">Trailer</a>` +
		`<script>bad()</script><a href="javascript:evil()">skip</a>`
	page := strings.Replace(stackPage, "First paragraph.<br><br>Second &amp; final.", notes, 1)
	items, err := (malstack38814.Flow{}).Extract(rfs.Page(page))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	// The stored payload's wire format is pinned here on purpose: its keys and
	// version tag are a contract that should break tests before breaking
	// baselines. Production decodes through the single canonical type.
	var stored struct {
		Version int      `json:"version"`
		Notes   []string `json:"notes"`
	}
	if err := json.Unmarshal([]byte(items[0].Description), &stored); err != nil {
		t.Fatalf("comparison payload is not valid JSON: %v", err)
	}
	if stored.Version != 1 {
		t.Fatalf("unexpected comparison version: %d", stored.Version)
	}
	want := []string{"Line one.", "", "Line two & more. Trailer (https://example.com/x) skip"}
	if len(stored.Notes) != len(want) {
		t.Fatalf("notes = %#v, want %#v", stored.Notes, want)
	}
	for i := range want {
		if stored.Notes[i] != want[i] {
			t.Fatalf("note line %d = %q, want %q", i, stored.Notes[i], want[i])
		}
	}
}

func TestFlowTakesEntryCountFromTrailingTotal(t *testing.T) {
	page := stackHTML(2,
		testEntry{id: "59878", title: "Alpha", notes: "Alpha note"},
		testEntry{id: "60000", title: "Beta", notes: "Beta note"},
	)
	// The curator's own description may say "12 Entries"; MAL's appended
	// total is what the parser must trust.
	page = strings.Replace(page,
		`content="MyAnimeList - Interest Stacks - 2 Entries, 270 Restacks"`,
		`content="Curator's picks: 12 Entries worth rewatching. MyAnimeList - Interest Stacks - 2 Entries, 270 Restacks"`, 1)
	items, err := (malstack38814.Flow{}).Extract(rfs.Page(page))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("declared count was confused by the curator's mention: got %d items", len(items))
	}
}

func TestFlowRejectsCardWithoutNotesWrapper(t *testing.T) {
	page := strings.Replace(stackPage, `<div class="intro">First paragraph.<br><br>Second &amp; final.</div>`, "", 1)
	if _, err := (malstack38814.Flow{}).Extract(rfs.Page(page)); err == nil {
		t.Fatal("expected a card without a recognized notes wrapper to be rejected")
	}
}

func TestFlowRejectsIncompleteOrUnrecognizedPages(t *testing.T) {
	cases := []struct {
		name string
		page string
	}{
		{"wrong stack", strings.Replace(stackPage, "stacks/38814", "stacks/99999", 1)},
		{"other watched stack", strings.Replace(stackPage, "stacks/38814", "stacks/82158", 1)},
		{"declared count mismatch", strings.Replace(stackPage, "1 Entries", "2 Entries", 1)},
		{"missing anime list", strings.Replace(stackPage, `<div class="list-anime-list">`, "<div>", 1)},
		{"missing stack detail", strings.Replace(stackPage, "content-left stacks-detail", "content-left", 1)},
		{"challenge page", `<html><body><form action="/login.php"><input name="login"></form></body></html>`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := (malstack38814.Flow{}).Extract(rfs.Page(tc.page)); err == nil {
				t.Fatal("expected the page to be rejected, got a complete observation")
			}
		})
	}
}

func TestFlowExtractsOnlyStackAnime(t *testing.T) {
	items, err := (malstack38814.Flow{}).Extract(rfs.Page(stackPage))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected only the stack's anime, got %d items", len(items))
	}
	item := items[0]
	if item.GUID != "59878" || item.Title != "Agami" || item.Link != "https://myanimelist.net/anime/59878" {
		t.Fatalf("unexpected anime identity: GUID=%q title=%q link=%q", item.GUID, item.Title, item.Link)
	}
	if item.PubDate != nil {
		t.Fatal("upstream has no item publication date; expected observation-time fallback")
	}
}
