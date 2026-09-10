package arrivaudine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ppowo/rfs/internal/rfs"
)

func fixture(t *testing.T, name string) rfs.Page {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return rfs.Page(data)
}

func TestParseNoticesReadsTheNoticeFeed(t *testing.T) {
	list, err := ParseNotices(fixture(t, "notices_20260910.json"))
	if err != nil {
		t.Fatalf("ParseNotices: %v", err)
	}
	if len(list) != 10 {
		t.Fatalf("parsed %d notices, want the 10 notices the feed window carries: %#v", len(list), list)
	}
	want := []struct {
		id, title, date string
	}{
		{"https://www.arrivaudine.it/notice/avviso-di-sciopero-di-4-ore-per-il-giorno-10-settembre-2026/", "Avviso di sciopero di 4 ore per il giorno 10 settembre 2026", "04/09/2026"},
		{"https://www.arrivaudine.it/notice/area-udinese-variazioni-sui-servizi-dal-6-luglio/", "Novità dal 6 luglio sui servizi extraurbani", "03/07/2026"},
	}
	byID := make(map[string]int, len(list))
	for i, notice := range list {
		byID[notice.ID] = i
	}
	for _, expected := range want {
		position, ok := byID[expected.id]
		if !ok {
			t.Fatalf("missing %s in %#v", expected.id, list)
		}
		notice := list[position]
		if notice.Title != expected.title {
			t.Fatalf("title = %q, want %q", notice.Title, expected.title)
		}
		if notice.Date != expected.date {
			t.Fatalf("date = %q, want %q", notice.Date, expected.date)
		}
		if notice.Link != expected.id {
			t.Fatalf("link = %q, want the notice permalink %q", notice.Link, expected.id)
		}
	}
}

func TestParseNoticesRejectsAResponseWithoutNotices(t *testing.T) {
	for name, page := range map[string]rfs.Page{
		"rest error": rfs.Page(`{"code":"rest_no_route","message":"No route was found"}`),
		"empty list": rfs.Page("[]"),
		"empty body": rfs.Page(""),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseNotices(page); err == nil {
				t.Fatalf("ParseNotices accepted %s", name)
			}
		})
	}
}
