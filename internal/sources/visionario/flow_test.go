package visionario

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kitevi/rfs/internal/rfs"
)

func TestExtractReturnsArchiveFilmsInOrder(t *testing.T) {
	page := rfs.Page(`
		<html><body>
		<article class="post-1 films type-films">
			<a href="https://visionario.movie/films/primetime/"><img alt=""></a>
			<h2 class="entry-title"><a href="https://visionario.movie/films/primetime/">Primetime</a></h2>
		</article>
		<article class="post-2 films type-films">
			<h2 class="entry-title"><a href="/films/je-so-pazzo/">Je so&#8217; pazzo</a></h2>
		</article>
		<a href="https://visionario.movie/films/page/2/">Older</a>
		</body></html>`)
	items, err := Flow{}.Extract(page)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("extracted %d items, want 2: %#v", len(items), items)
	}
	if items[0].GUID != guidPrefix+"primetime" || items[0].Title != "Primetime" || items[0].Link != "https://visionario.movie/films/primetime/" {
		t.Fatalf("first item = %#v", items[0])
	}
	if items[1].GUID != guidPrefix+"je-so-pazzo" || items[1].Title != "Je so’ pazzo" || items[1].Link != "https://visionario.movie/films/je-so-pazzo/" {
		t.Fatalf("second item = %#v", items[1])
	}
}

func TestExtractCollapsesDuplicateFilms(t *testing.T) {
	page := rfs.Page(`
		<article class="type-films"><h2 class="entry-title"><a href="/films/primetime/">Primetime</a></h2></article>
		<article class="type-films"><h2 class="entry-title"><a href="/films/primetime/">Primetime again</a></h2></article>`)
	items, err := Flow{}.Extract(page)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(items) != 1 || items[0].Title != "Primetime" {
		t.Fatalf("items = %#v, want the first Primetime only", items)
	}
}

func TestExtractRejectsPagesWithoutFilms(t *testing.T) {
	_, err := Flow{}.Extract(rfs.Page(`<html><body><p>Nothing to see here</p><a href="/films/page/2/">Older</a></body></html>`))
	if err == nil || !strings.Contains(err.Error(), "no films") {
		t.Fatalf("error = %v, want a rejection", err)
	}
}

func TestExtractRejectsAFilmArticleWithoutAFilmLink(t *testing.T) {
	page := rfs.Page(`<article class="type-films"><h2 class="entry-title"><a href="https://example.com/films/primetime/">Primetime</a></h2></article>`)
	_, err := Flow{}.Extract(page)
	if err == nil || !strings.Contains(err.Error(), "links to") {
		t.Fatalf("error = %v, want a link rejection", err)
	}
}

func TestFirstObservationPublishesNothing(t *testing.T) {
	flow := Flow{}
	items := archiveItems(t, "primetime", "dune-parte-tre")
	ready, pending, err := flow.PlanIdentities(nil, items)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending = %#v, want no film-page fetches on the baseline", pending)
	}
	decision, err := flow.Evaluate(nil, ready)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(decision.Announcements) != 0 {
		t.Fatalf("announcements = %#v, want none", decision.Announcements)
	}
	state := decodeSeen(t, decision.Checkpoint)
	if len(state) != 2 || state[0] != "dune-parte-tre" || state[1] != "primetime" {
		t.Fatalf("seen = %#v", state)
	}
}

func TestLaterObservationAnnouncesOnlyTheNewRelease(t *testing.T) {
	flow := Flow{}
	first, err := flow.Evaluate(nil, archiveItems(t, "primetime"))
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}
	current := archiveItems(t, "wild-horse-nine-v-o", "primetime", "ultra")
	ready, pending, err := flow.PlanIdentities(first.Checkpoint, current)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(pending) != 2 || pending[0].GUID != guidPrefix+"wild-horse-nine-v-o" || pending[1].GUID != guidPrefix+"ultra" {
		t.Fatalf("pending = %#v", pending)
	}
	pages := map[string]rfs.Page{
		filmURL("wild-horse-nine-v-o"): filmPage("novità prima visione visionario", "dal 28 Ott 2026", "Visionario", "Centrale"),
		filmURL("ultra"):               filmPage("evento visionario", "dal 16 Ott 2026", "Visionario"),
	}
	ready = resolvePending(t, flow, ready, pending, pages)
	decision, err := flow.Evaluate(first.Checkpoint, ready)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(decision.Announcements) != 1 {
		t.Fatalf("announcements = %#v, want the new release only", decision.Announcements)
	}
	got := decision.Announcements[0]
	if got.GUID != guidPrefix+"wild-horse-nine-v-o" || got.Title != "wild-horse-nine-v-o" {
		t.Fatalf("announcement identity = %#v", got)
	}
	if got.Link != filmURL("wild-horse-nine-v-o") || got.Description != "dal 28 Ott 2026, Visionario, Centrale" {
		t.Fatalf("announcement = %#v", got)
	}
	if got.Metadata != "" {
		t.Fatalf("announcement kept identity metadata: %q", got.Metadata)
	}
	state := decodeSeen(t, decision.Checkpoint)
	if len(state) != 3 || state[0] != "primetime" || state[1] != "ultra" || state[2] != "wild-horse-nine-v-o" {
		t.Fatalf("seen = %#v, want the event remembered and the release announced", state)
	}

	again, err := flow.Evaluate(decision.Checkpoint, ready)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if len(again.Announcements) != 0 {
		t.Fatalf("replay announcements = %#v, want none", again.Announcements)
	}
}

func TestReleaseWithoutAnOpeningDateFails(t *testing.T) {
	item := rfs.ExtractedItem{GUID: guidPrefix + "primetime", Title: "Primetime", Link: filmURL("primetime")}
	page := rfs.Page(`<div class="primaVisione">prima visione visionario</div><div class="innerInfo">Visionario</div>`)
	if _, err := (Flow{}).ResolveIdentity(page, item); err == nil || !strings.Contains(err.Error(), "no opening date") {
		t.Fatalf("error = %v, want a missing opening date", err)
	}
}

func TestUnrecognizedBadgeFails(t *testing.T) {
	item := rfs.ExtractedItem{GUID: guidPrefix + "primetime", Title: "Primetime", Link: filmURL("primetime")}
	page := filmPage("rassegna visionario", "dal 16 Ott 2026", "Visionario")
	if _, err := (Flow{}).ResolveIdentity(page, item); err == nil || !strings.Contains(err.Error(), "unrecognized badge") {
		t.Fatalf("error = %v, want an unrecognized badge", err)
	}
}

func TestUnclassifiedFilmFailsTheLaterObservation(t *testing.T) {
	flow := Flow{}
	first, err := flow.Evaluate(nil, archiveItems(t, "primetime"))
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}
	_, err = flow.Evaluate(first.Checkpoint, archiveItems(t, "primetime", "dune-parte-tre"))
	if err == nil || !strings.Contains(err.Error(), "was not classified") {
		t.Fatalf("error = %v, want an unclassified film", err)
	}
}

func TestUnsupportedCheckpointVersionFails(t *testing.T) {
	_, err := (Flow{}).Evaluate(json.RawMessage(`{"version":99,"seen":["primetime"]}`), archiveItems(t, "primetime"))
	if err == nil || !strings.Contains(err.Error(), "unsupported checkpoint version") {
		t.Fatalf("error = %v, want a version rejection", err)
	}
}

func archiveItems(t *testing.T, slugs ...string) []rfs.ExtractedItem {
	t.Helper()
	var builder strings.Builder
	builder.WriteString("<html><body>")
	for _, slug := range slugs {
		builder.WriteString(`<article class="type-films"><h2 class="entry-title"><a href="/films/` + slug + `/">` + slug + `</a></h2></article>`)
	}
	builder.WriteString("</body></html>")
	items, err := Flow{}.Extract(rfs.Page(builder.String()))
	if err != nil {
		t.Fatalf("extract archive: %v", err)
	}
	return items
}

func filmPage(badge, opening string, halls ...string) rfs.Page {
	var builder strings.Builder
	builder.WriteString(`<div class="primaVisione">` + badge + `</div>`)
	builder.WriteString(`<div class="inizio">` + opening + `</div>`)
	builder.WriteString(`<div class="innerInfo"><strong>VEN</strong></div>`)
	builder.WriteString(`<div class="innerInfo">16/10/2026</div>`)
	for _, hall := range halls {
		builder.WriteString(`<div class="col-xs-3 innerInfo">` + hall + `</div>`)
	}
	return rfs.Page(builder.String())
}

func resolvePending(t *testing.T, flow Flow, ready, pending []rfs.ExtractedItem, pages map[string]rfs.Page) []rfs.ExtractedItem {
	t.Helper()
	for _, item := range pending {
		url, err := flow.IdentityURL(item)
		if err != nil {
			t.Fatalf("identity url: %v", err)
		}
		page, ok := pages[url]
		if !ok {
			t.Fatalf("no fixture for %s", url)
		}
		resolved, err := flow.ResolveIdentity(page, item)
		if err != nil {
			t.Fatalf("resolve %s: %v", item.GUID, err)
		}
		if resolved.GUID != item.GUID {
			t.Fatalf("resolve changed GUID %q to %q", item.GUID, resolved.GUID)
		}
		found := false
		for i := range ready {
			if ready[i].GUID == item.GUID {
				ready[i] = resolved
				found = true
			}
		}
		if !found {
			t.Fatalf("pending %s was not in the observation", item.GUID)
		}
	}
	return ready
}

func decodeSeen(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	var state checkpoint
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("decode checkpoint: %v", err)
	}
	if state.Version != CheckpointVersion {
		t.Fatalf("checkpoint version = %d", state.Version)
	}
	return state.Seen
}
