package aptgorizia

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ppowo/rfs/internal/rfs"
	"github.com/ppowo/rfs/internal/sources/notices"
)

func fixture(t *testing.T, name string) rfs.Page {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return rfs.Page(data)
}

func TestParseNoticesKeepsTheActiveNotices(t *testing.T) {
	list, err := ParseNotices(fixture(t, "avvisi_20260910.html"))
	if err != nil {
		t.Fatalf("ParseNotices: %v", err)
	}
	if len(list) != 6 {
		t.Fatalf("parsed %d notices, want the 6 notices the summary page lists: %#v", len(list), list)
	}
	byID := make(map[string]notices.Notice, len(list))
	for _, notice := range list {
		byID[notice.ID] = notice
	}
	want := []struct {
		id    string
		title string
	}{
		{"https://www.aptgorizia.it/avvisi-home/moraro-fermate-sospese-per-processione-il-08-09-2026/", "Moraro, fermate sospese per processione il 08/09/2026"},
		{"https://www.aptgorizia.it/deviazioni-di-percorso/ronchi-dei-legionari-fermate-sospese-per-rd-e-rs-a-causa-lavori-dal-28-08-2026/", "Ronchi dei Legionari, fermate sospese per RD e RS a causa lavori dal 28/08/2026"},
		{"https://www.aptgorizia.it/deviazioni-di-percorso/ronchi-dei-legionari-via-del-capitello-2-fermata-sospesa/", "Ronchi dei Legionari via del Capitello 2 – fermata sospesa dal 29-06-2026"},
	}
	for _, expected := range want {
		notice, ok := byID[expected.id]
		if !ok {
			t.Fatalf("missing %s in %#v", expected.id, list)
		}
		if notice.Title != expected.title {
			t.Fatalf("title = %q, want %q", notice.Title, expected.title)
		}
		if notice.Link != expected.id {
			t.Fatalf("link = %q, want the notice permalink %q", notice.Link, expected.id)
		}
	}
}

func TestParseNoticesRejectsAPageWithoutTheCollection(t *testing.T) {
	for name, page := range map[string]rfs.Page{
		"error page": rfs.Page("<html><body><h1>Page not found</h1></body></html>"),
		"empty body": rfs.Page(""),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseNotices(page); err == nil {
				t.Fatalf("ParseNotices accepted %s", name)
			}
		})
	}
}
