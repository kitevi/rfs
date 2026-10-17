package cittafiera

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/kitevi/rfs/internal/rfs"
)

func TestExtractStripsTheFormatBadge(t *testing.T) {
	page := rfs.Page(`
		<a href="/scheda-film/?id_pro=2866"><img alt=""></a>
		<a href="/scheda-film/?id_pro=2866" style="font-size: 1.5em">Coyote Vs Acme <span class="properties">35MM</span></a>
		<a href="/scheda-film/?id_pro=2866">Scopri di più</a>
		<a href="/scheda-film/?id_pro=2936">Avengers: Doomsday <span class="properties">35MM,14+</span></a>`)
	items, err := Flow{}.Extract(page)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("extracted %d items, want 2: %#v", len(items), items)
	}
	if items[0].GUID != guidPrefix+"2866" || items[0].Title != "Coyote Vs Acme" || items[0].Link != filmURL("2866") {
		t.Fatalf("first item = %#v", items[0])
	}
	if items[1].GUID != guidPrefix+"2936" || items[1].Title != "Avengers: Doomsday" {
		t.Fatalf("second item = %#v", items[1])
	}
}

func TestExtractRejectsPagesWithoutFilms(t *testing.T) {
	_, err := Flow{}.Extract(rfs.Page(`<html><body><p>Nothing to see here</p></body></html>`))
	if err == nil || !strings.Contains(err.Error(), "no films") {
		t.Fatalf("error = %v, want a rejection", err)
	}
}

func TestFirstObservationAnnouncesFilmsInsideTwoWeeks(t *testing.T) {
	flow := Flow{}
	now := romeTime(2026, 10, 16)
	items := homeItems(t, "2866", "2936")
	ready, pending, err := flow.PlanIdentities(nil, items)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("pending = %d, want both films classified", len(pending))
	}
	ready = resolvePending(t, flow, ready, pending, map[string]rfs.Page{
		filmURL("2866"): filmPage("2866", "Dave Green", "01:44", "2026-10-21"),
		filmURL("2936"): filmPage("2936", "Anthony Russo", "02:30", "2026-12-16", "2026-12-20"),
	})
	decision, err := flow.EvaluateAt(now, nil, ready)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if len(decision.Announcements) != 1 {
		t.Fatalf("announcements = %#v, want only the October film", decision.Announcements)
	}
	got := decision.Announcements[0]
	if got.GUID != guidPrefix+"2866" || got.Description != "21 Ott 2026, Dave Green, 01:44" || got.Metadata != "" {
		t.Fatalf("announcement = %#v", got)
	}
	announced, held := decodeState(t, decision.Checkpoint)
	if len(announced) != 1 || announced[0] != "2866" {
		t.Fatalf("announced = %#v", announced)
	}
	if held["2936"] != "2026-12-16" || len(held) != 1 {
		t.Fatalf("held = %#v, want Doomsday until December", held)
	}
}

func TestHeldFilmIsAnnouncedTwoWeeksBefore(t *testing.T) {
	flow := Flow{}
	first := decide(t, flow, romeTime(2026, 10, 16), nil, homeItems(t, "2936"), map[string]rfs.Page{
		filmURL("2936"): filmPage("2936", "Anthony Russo", "02:30", "2026-12-16", "2026-12-20"),
	})
	if len(first.Announcements) != 0 {
		t.Fatalf("October announcements = %#v, want none", first.Announcements)
	}

	// 1 December 23:00 in Rome is still one day too early.
	early := decide(t, flow, time.Date(2026, 12, 1, 22, 0, 0, 0, time.UTC), first.Checkpoint, homeItems(t, "2936"), map[string]rfs.Page{
		filmURL("2936"): filmPage("2936", "Anthony Russo", "02:30", "2026-12-16"),
	})
	if len(early.Announcements) != 0 {
		t.Fatalf("1 December announcements = %#v, want none", early.Announcements)
	}

	// 2 December 00:30 in Rome is the first day inside the window.
	due := decide(t, flow, time.Date(2026, 12, 1, 23, 30, 0, 0, time.UTC), early.Checkpoint, homeItems(t, "2936"), map[string]rfs.Page{
		filmURL("2936"): filmPage("2936", "Anthony Russo", "02:30", "2026-12-20", "2026-12-16"),
	})
	if len(due.Announcements) != 1 || due.Announcements[0].Description != "16 Dic 2026, Anthony Russo, 02:30" {
		t.Fatalf("2 December announcement = %#v", due.Announcements)
	}
	if due.Announcements[0].Metadata != "" {
		t.Fatalf("announcement kept metadata %q", due.Announcements[0].Metadata)
	}

	again := decide(t, flow, time.Date(2026, 12, 1, 23, 30, 0, 0, time.UTC), due.Checkpoint, homeItems(t, "2936"), nil)
	if len(again.Announcements) != 0 {
		t.Fatalf("replay announcements = %#v, want none", again.Announcements)
	}
}

func TestHeldFilmThatLeavesTheHomepageIsDropped(t *testing.T) {
	flow := Flow{}
	first := decide(t, flow, romeTime(2026, 10, 16), nil, homeItems(t, "2866", "2936"), map[string]rfs.Page{
		filmURL("2866"): filmPage("2866", "Dave Green", "01:44", "2026-10-21"),
		filmURL("2936"): filmPage("2936", "Anthony Russo", "02:30", "2026-12-16"),
	})
	next := decide(t, flow, romeTime(2026, 10, 16), first.Checkpoint, homeItems(t, "2866"), map[string]rfs.Page{
		filmURL("2866"): filmPage("2866", "Dave Green", "01:44", "2026-10-21"),
	})
	if len(next.Announcements) != 0 {
		t.Fatalf("announcements = %#v, want none", next.Announcements)
	}
	_, held := decodeState(t, next.Checkpoint)
	if len(held) != 0 {
		t.Fatalf("held = %#v, want the departed film dropped", held)
	}
}

func TestFilmPageSkipsABlankDayQuery(t *testing.T) {
	item := rfs.ExtractedItem{GUID: guidPrefix + "2866", Title: "Coyote Vs Acme", Link: filmURL("2866")}
	page := rfs.Page(`<a href="/scheda-film?d=+&id_pro=2866">blank</a>` + string(filmPage("2866", "Dave Green", "01:44", "2026-10-21")))
	resolved, err := (Flow{}).ResolveIdentity(page, item)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved.GUID != item.GUID || !strings.Contains(resolved.Metadata, "2026-10-21") {
		t.Fatalf("resolved = %#v, want the October day", resolved)
	}
}

func TestFilmPageWithoutADayFails(t *testing.T) {
	item := rfs.ExtractedItem{GUID: guidPrefix + "2866", Title: "Coyote Vs Acme", Link: filmURL("2866")}
	page := rfs.Page(`<p><strong>Regista: </strong>Dave Green</p><p><strong>Durata: </strong>01:44</p>`)
	if _, err := (Flow{}).ResolveIdentity(page, item); err == nil || !strings.Contains(err.Error(), "no day") {
		t.Fatalf("error = %v, want a missing day", err)
	}
}

func TestUnclassifiedFilmFails(t *testing.T) {
	_, err := (Flow{}).EvaluateAt(romeTime(2026, 10, 16), nil, []rfs.ExtractedItem{{
		GUID:  guidPrefix + "2866",
		Title: "Coyote Vs Acme",
		Link:  filmURL("2866"),
	}})
	if err == nil || !strings.Contains(err.Error(), "was not classified") {
		t.Fatalf("error = %v, want an unclassified film", err)
	}
}

func homeItems(t *testing.T, ids ...string) []rfs.ExtractedItem {
	t.Helper()
	var builder strings.Builder
	for _, id := range ids {
		builder.WriteString(`<a href="/scheda-film/?id_pro=` + id + `">` + id + ` <span class="properties">35MM</span></a>`)
	}
	items, err := Flow{}.Extract(rfs.Page(builder.String()))
	if err != nil {
		t.Fatalf("extract home: %v", err)
	}
	return items
}

func filmPage(id, director, duration string, days ...string) rfs.Page {
	var builder strings.Builder
	builder.WriteString(`<p><strong>Durata: </strong>` + duration + `</p>`)
	builder.WriteString(`<p><strong>Regista: </strong>` + director + `</p>`)
	for _, day := range days {
		builder.WriteString(`<a href="/scheda-film?d=` + day + ` 00:00:00&id_pro=` + id + `#prog">` + day + `</a>`)
	}
	return rfs.Page(builder.String())
}

func decide(t *testing.T, flow Flow, now time.Time, checkpoint json.RawMessage, items []rfs.ExtractedItem, pages map[string]rfs.Page) rfs.AnnouncementDecision {
	t.Helper()
	ready, pending, err := flow.PlanIdentities(checkpoint, items)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	ready = resolvePending(t, flow, ready, pending, pages)
	decision, err := flow.EvaluateAt(now, checkpoint, ready)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	return decision
}

func resolvePending(t *testing.T, flow Flow, ready, pending []rfs.ExtractedItem, pages map[string]rfs.Page) []rfs.ExtractedItem {
	t.Helper()
	for _, item := range pending {
		pageURL, err := flow.IdentityURL(item)
		if err != nil {
			t.Fatalf("identity url: %v", err)
		}
		page, ok := pages[pageURL]
		if !ok {
			t.Fatalf("no fixture for %s", pageURL)
		}
		resolved, err := flow.ResolveIdentity(page, item)
		if err != nil {
			t.Fatalf("resolve %s: %v", item.GUID, err)
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

func decodeState(t *testing.T, raw json.RawMessage) (announced []string, held map[string]string) {
	t.Helper()
	var state checkpoint
	if err := json.Unmarshal(raw, &state); err != nil {
		t.Fatalf("decode checkpoint: %v", err)
	}
	if state.Version != CheckpointVersion {
		t.Fatalf("checkpoint version = %d", state.Version)
	}
	return state.Announced, state.Held
}

func romeTime(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 12, 0, 0, 0, rome)
}
