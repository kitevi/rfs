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
		id, title, published, validity string
	}{
		// The headline states a start without a year. The phrase is shown as
		// published; no year is invented for it.
		{"https://www.triestetrasporti.it/it/orario-invernale-14settembre2026", "Dal 14 settembre in vigore l'orario invernale degli autobus", "10/09/2026", "Dal 14 settembre"},
		{"https://www.triestetrasporti.it/it/maltempo-10settembre-deviazioni", "Maltempo, tutte le deviazioni in vigore", "10/09/2026", ""},
		{"https://www.triestetrasporti.it/it/linea33/-servizio-spola", "Linea 33/, sospeso il servizio minibus e riattivata la spola via della Bastia-Campanelle", "09/09/2026", ""},
		{"https://www.triestetrasporti.it/it/modifiche-servizio-chiusura-santanastasio", "Chiusura di via Sant'Anastasio causa lavori: deviazione per le linee 28, 64 e 30", "20/08/2026", "dal 24/08/2026"},
		{"https://www.triestetrasporti.it/it/abbonamenti-scolastici-agevolati-acquisto-24-agosto-2026", "Abbonamenti scolastici agevolati, acquisto possibile da lunedì 24 agosto", "21/08/2026", "fino al 31/10/2026"},
	}
	for _, expected := range want {
		notice, ok := byID[expected.id]
		if !ok {
			t.Fatalf("missing %s in %#v", expected.id, list)
		}
		if notice.Title != expected.title {
			t.Fatalf("title = %q, want %q", notice.Title, expected.title)
		}
		if got := notice.Published.Display(); got != expected.published {
			t.Fatalf("%s published = %q, want %q", expected.id, got, expected.published)
		}
		if got := notice.Validity.Display(); got != expected.validity {
			t.Fatalf("%s validity = %q, want %q", expected.id, got, expected.validity)
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
