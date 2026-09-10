package notices

import (
	"strings"
	"testing"

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
	return Notice{ID: id, Link: id, Title: title, Summary: summary, Date: "10 Settembre 2026"}
}

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
	changes, err := flow.Changes(before, after)
	if err != nil {
		t.Fatalf("Changes: %v", err)
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
	if changes, err := flow.Changes(before, retitled); err != nil || len(changes) != 1 {
		t.Fatalf("emitted %#v (err %v), want only the new notice", changes, err)
	}
	if changes, err := flow.Changes(before, before); err != nil || len(changes) != 0 {
		t.Fatalf("an unchanged poll emitted %#v (err %v)", changes, err)
	}
	if changes, err := flow.Changes(before, nil); err != nil || len(changes) != 0 {
		t.Fatalf("a withdrawn notice emitted %#v (err %v), want nothing", changes, err)
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
