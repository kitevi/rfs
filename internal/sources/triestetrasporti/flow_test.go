package triestetrasporti

import (
	"os"
	"path/filepath"
	"strings"
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

func parse(t *testing.T, page rfs.Page) []notices.Notice {
	t.Helper()
	list, err := ParseNotices(page)
	if err != nil {
		t.Fatalf("ParseNotices: %v", err)
	}
	return list
}

func index(list []notices.Notice) map[string]notices.Notice {
	byID := make(map[string]notices.Notice, len(list))
	for _, notice := range list {
		byID[notice.ID] = notice
	}
	return byID
}

func TestParseNoticesKeepsTheInForceListOnly(t *testing.T) {
	list := parse(t, fixture(t, "avvisi_20260910.html"))
	if len(list) != 9 {
		t.Fatalf("parsed %d notices, want the 9 in-force notices: %#v", len(list), list)
	}
	byID := index(list)
	want := []struct {
		id    string
		title string
		date  string
	}{
		{"https://www.triestetrasporti.it/it/orario-invernale-14settembre2026", "Dal 14 settembre in vigore l'orario invernale degli autobus", "10/09/2026"},
		{"https://www.triestetrasporti.it/it/maltempo-10settembre-deviazioni", "Maltempo, tutte le deviazioni in vigore", "10/09/2026"},
		{"https://www.triestetrasporti.it/it/linea33/-servizio-spola", "Linea 33/, sospeso il servizio minibus e riattivata la spola via della Bastia-Campanelle", "09/09/2026"},
		{"https://www.triestetrasporti.it/it/modifiche-servizio-chiusura-santanastasio", "Chiusura di via Sant'Anastasio causa lavori: deviazione per le linee 28, 64 e 30", "20/08/2026"},
	}
	for _, expected := range want {
		notice, ok := byID[expected.id]
		if !ok {
			t.Fatalf("missing %s in %#v", expected.id, list)
		}
		if notice.Title != expected.title {
			t.Fatalf("title = %q, want %q", notice.Title, expected.title)
		}
		if notice.Date != expected.date {
			t.Fatalf("%s date = %q, want %q", expected.id, notice.Date, expected.date)
		}
		if notice.Link != expected.id {
			t.Fatalf("link = %q, want the notice permalink %q", notice.Link, expected.id)
		}
		if strings.TrimSpace(notice.Summary) == "" {
			t.Fatalf("%s carries no lead text", expected.id)
		}
	}
}

func TestParseNoticesRejectsAPageWithoutTheArchiveMarker(t *testing.T) {
	page := string(fixture(t, "avvisi_20260910.html"))
	page = strings.Replace(page, `id="archivio-avvisi"`, `id="avvisi-infomobilita"`, 1)
	if _, err := ParseNotices(rfs.Page(page)); err == nil {
		t.Fatal("ParseNotices accepted a page whose expired notices are indistinguishable")
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
