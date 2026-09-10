package notices

import (
	"strings"
	"testing"
	"time"

	"github.com/ppowo/rfs/internal/rfs"
)

// stubList serves fixed notices so these tests describe the change-feed
// behaviour rather than any operator's markup.
type stubList struct{ notices []Notice }

func (s stubList) Notices(rfs.Page) ([]Notice, error) { return s.notices, nil }

func flowWith(notices ...Notice) Flow {
	return Flow{Operator: "Arriva Udine", Parser: stubList{notices: notices}.Notices}
}

func extract(t *testing.T, flow Flow) []rfs.ExtractedItem {
	t.Helper()
	items, err := flow.Extract(rfs.Page("<page>"))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	return items
}

func notice(id, title, summary string) Notice {
	return Notice{ID: id, Link: id, Title: title, Summary: summary, Published: dayDate(2026, time.September, 10)}
}

// polledAt is the fixed observation instant these behaviour tests compare at.
func polledAt() time.Time { return romeTime(2026, time.September, 10, 12, 0) }

func TestExtractAnnouncesEveryNoticeWithItsOperator(t *testing.T) {
	items := extract(t, flowWith(notice("https://example.com/a", "Linea 9 deviata", "Deviazione in via Carducci.")))
	if len(items) != 1 {
		t.Fatalf("extracted %#v, want one notice", items)
	}
	if items[0].GUID != "https://example.com/a" || items[0].Link != "https://example.com/a" {
		t.Fatalf("GUID/Link = %q/%q, want the notice permalink as identity", items[0].GUID, items[0].Link)
	}
	if !strings.HasPrefix(items[0].Title, "[Bus · Arriva Udine] ") {
		t.Fatalf("title = %q, want the operator scope label", items[0].Title)
	}
}

func TestChangesAnnouncesUpdatesWithoutLosingIdentity(t *testing.T) {
	flow := flowWith(notice("https://example.com/a", "Linea 9 deviata", "Deviazione in via Carducci."))
	before := extract(t, flow)
	after := extract(t, flowWith(notice("https://example.com/a", "Linea 9 deviata", "Deviazione fino a lunedì.")))
	changes, err := flow.ChangesAt(polledAt(), before, after)
	if err != nil {
		t.Fatalf("ChangesAt: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("emitted %#v, want one update", changes)
	}
	if changes[0].GUID != "https://example.com/a" {
		t.Fatalf("GUID = %q, want the unchanged identity", changes[0].GUID)
	}
	if !strings.HasPrefix(changes[0].Title, "[Aggiornato · Bus · Arriva Udine] ") {
		t.Fatalf("title = %q, want an update label", changes[0].Title)
	}
	if !strings.Contains(changes[0].Description, "via Carducci") || !strings.Contains(changes[0].Description, "fino a lunedì") {
		t.Fatalf("description = %q, want the changed summary", changes[0].Description)
	}
}

func TestChangesIgnoresUnchangedAndDisappearedNotices(t *testing.T) {
	flow := flowWith(notice("https://example.com/a", "Linea 9 deviata", "Deviazione."))
	before := extract(t, flow)
	retitled := extract(t, flowWith(notice("https://example.com/b", "Linea 4 sospesa", "Servizio sospeso.")))
	if changes, err := flow.ChangesAt(polledAt(), before, retitled); err != nil || len(changes) != 1 {
		t.Fatalf("emitted %#v (err %v), want only the new notice", changes, err)
	}
	if changes, err := flow.ChangesAt(polledAt(), before, before); err != nil || len(changes) != 0 {
		t.Fatalf("an unchanged poll emitted %#v (err %v)", changes, err)
	}
	if changes, err := flow.ChangesAt(polledAt(), before, nil); err != nil || len(changes) != 0 {
		t.Fatalf("a withdrawn notice emitted %#v (err %v), want nothing", changes, err)
	}
}

func TestChangesAtAnnouncesOnlyTheNoticesThatAreStillNewsAtThePoll(t *testing.T) {
	fresh := notice("https://example.com/fresh", "Linea 9 deviata", "Deviazione.")
	fresh.Published = dayDate(2026, time.July, 10)
	old := notice("https://example.com/old", "Linea 4 sospesa", "Servizio sospeso.")
	old.Published = dayDate(2026, time.July, 9)

	flow := flowWith(fresh, old)
	changes, err := flow.ChangesAt(polledAt(), nil, extract(t, flow))
	if err != nil {
		t.Fatalf("ChangesAt: %v", err)
	}
	if len(changes) != 1 || changes[0].GUID != "https://example.com/fresh" {
		t.Fatalf("announced %#v, want only the notice inside the freshness window", changes)
	}
}

func TestChangesAtSuppressesEndedNoticesAndKeepsOpenOnes(t *testing.T) {
	ended := notice("https://example.com/ended", "Linea 9 deviata", "Deviazione.")
	ended.Validity = Validity{Start: dayDate(2026, time.August, 5), End: dayDate(2026, time.August, 5)}
	today := notice("https://example.com/today", "Linea 4 sospesa", "Servizio sospeso.")
	today.Validity = Validity{Start: dayDate(2026, time.September, 10), End: dayDate(2026, time.September, 10)}
	open := notice("https://example.com/open", "Linea 5 limitata", "Limitazione.")
	open.Validity = Validity{Start: dayDate(2022, time.February, 7)}

	flow := flowWith(ended, today, open)
	changes, err := flow.ChangesAt(polledAt(), nil, extract(t, flow))
	if err != nil {
		t.Fatalf("ChangesAt: %v", err)
	}
	announced := map[string]bool{}
	for _, change := range changes {
		announced[change.GUID] = true
	}
	if announced["https://example.com/ended"] {
		t.Fatalf("announced an ended notice: %#v", changes)
	}
	for _, want := range []string{"https://example.com/today", "https://example.com/open"} {
		if !announced[want] {
			t.Fatalf("did not announce %s: %#v", want, changes)
		}
	}
}

func TestChangesAtIgnoresEditsToEndedNoticesButAnnouncesExtensions(t *testing.T) {
	flow := flowWith(notice("https://example.com/a", "Linea 9 deviata", "Deviazione."))
	ended := notice("https://example.com/a", "Linea 9 deviata", "Deviazione.")
	ended.Published = dayDate(2026, time.August, 1)
	ended.Validity = Validity{Start: dayDate(2026, time.August, 5), End: dayDate(2026, time.August, 5)}
	before := extract(t, flowWith(ended))

	// An ordinary edit to a notice whose window has ended stays silent.
	edited := ended
	edited.Summary = "Deviazione prolungata fino a nuovo avviso."
	edited.Title = "Linea 9 deviata (aggiornato)"
	if changes, err := flow.ChangesAt(polledAt(), before, extract(t, flowWith(edited))); err != nil || len(changes) != 0 {
		t.Fatalf("emitted %#v (err %v), want nothing for an edit to an ended notice", changes, err)
	}

	// A window that now reaches into the future is news again.
	extended := ended
	extended.Validity.End = dayDate(2026, time.September, 30)
	changes, err := flow.ChangesAt(polledAt(), before, extract(t, flowWith(extended)))
	if err != nil {
		t.Fatalf("ChangesAt: %v", err)
	}
	if len(changes) != 1 || changes[0].GUID != "https://example.com/a" {
		t.Fatalf("emitted %#v, want one update for the extended window", changes)
	}
	if !strings.Contains(changes[0].Description, "30/09/2026") {
		t.Fatalf("description = %q, want the new window", changes[0].Description)
	}
}

func TestChangesAtUsesThePublicationTimeOnlyWhenItNamesOne(t *testing.T) {
	published := mintueDate(romeTime(2026, time.September, 4, 10, 6))
	noticeWith := func(published Date) Notice {
		return Notice{ID: "https://example.com/a", Link: "https://example.com/a", Title: "Linea 9 deviata", Published: published}
	}
	flow := flowWith(noticeWith(published))
	changes, err := flow.ChangesAt(polledAt(), nil, extract(t, flow))
	if err != nil {
		t.Fatalf("ChangesAt: %v", err)
	}
	if len(changes) != 1 || changes[0].PubDate == nil || !changes[0].PubDate.Equal(published.Time) {
		t.Fatalf("changes = %#v, want the notice's own publication time", changes)
	}

	// A publication date the page has not reached yet is not an instant either.
	flow = flowWith(noticeWith(mintueDate(romeTime(2026, time.September, 20, 9, 0))))
	changes, err = flow.ChangesAt(polledAt(), nil, extract(t, flow))
	if err != nil {
		t.Fatalf("ChangesAt: %v", err)
	}
	if len(changes) != 1 || changes[0].PubDate != nil {
		t.Fatalf("changes = %#v, want a future publication date refused", changes)
	}

	// A day-precision publication date is shown but never invented as an instant.
	flow = flowWith(noticeWith(dayDate(2026, time.September, 4)))
	changes, err = flow.ChangesAt(polledAt(), nil, extract(t, flow))
	if err != nil {
		t.Fatalf("ChangesAt: %v", err)
	}
	if len(changes) != 1 || changes[0].PubDate != nil {
		t.Fatalf("changes = %#v, want the observation time left to the engine", changes)
	}
	if !strings.Contains(changes[0].Description, "04/09/2026") || !strings.Contains(changes[0].Description, "Rilevato") {
		t.Fatalf("description = %q, want the published day and the discovery time", changes[0].Description)
	}
}

func TestExtractRejectsNoticesWithoutIdentity(t *testing.T) {
	if _, err := flowWith(Notice{Title: "Linea 9 deviata"}).Extract(rfs.Page("<page>")); err == nil {
		t.Fatal("Extract accepted a notice without a permalink")
	}
}

func TestExtractKeepsOneItemPerNotice(t *testing.T) {
	items := extract(t, flowWith(
		notice("https://example.com/a", "Linea 9 deviata", "Deviazione."),
		notice("https://example.com/a", "Linea 9 deviata", "Deviazione."),
	))
	if len(items) != 1 {
		t.Fatalf("extracted %#v, want a repeated notice once", items)
	}
}
