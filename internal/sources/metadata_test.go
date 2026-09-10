package sources_test

import (
	"context"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ppowo/rfs/internal/rfs"
)

func TestNoticeMetadataSurvivesPollStorageAndHTTP(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "feeds.db")
	store, err := rfs.OpenSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	source := sourceByID(t, "arriva-udine")
	page := fixture(t, "arrivaudine/testdata/notices_20260910.json")
	items := pollIntoStore(t, store, source, map[string]rfs.Page{source.URL: page})
	if len(items) != 2 {
		t.Fatalf("items = %d", len(items))
	}
	for _, item := range items {
		if item.Metadata == "" || !strings.Contains(item.Description, "Operatore:") {
			t.Fatalf("not a real persisted announcement: %+v", item)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = rfs.OpenSQLiteStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	reopened, err := store.LoadSnapshot(ctx, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	for i := range items {
		if items[i].Metadata != reopened[i].Metadata {
			t.Fatal("metadata lost on reopen")
		}
	}
	for _, ext := range []string{"html", "xml"} {
		handler := rfs.NewHTTPHandlerWithClock(store, []rfs.Source{source}, rfs.BuildInfo{}, feedClock{capturedAt()})
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest("GET", "/feeds/arriva-udine."+ext, nil))
		for _, date := range []string{"Pubblicato: 26/08/2026", "Pubblicato: 04/09/2026"} {
			if !strings.Contains(rec.Body.String(), date) {
				t.Fatalf("%s missing %s: %s", ext, date, rec.Body.String())
			}
		}
		if strings.Contains(rec.Body.String(), "periodo pasquale") {
			t.Fatal("Easter is served")
		}
	}
	// No intervening poll: the request clock alone ages these announcements out.
	later := rfs.NewHTTPHandlerWithClock(store, []rfs.Source{source}, rfs.BuildInfo{}, feedClock{time.Date(2026, 12, 1, 12, 0, 0, 0, time.UTC)})
	rec := httptest.NewRecorder()
	later.ServeHTTP(rec, httptest.NewRequest("GET", "/feeds/arriva-udine.xml", nil))
	if strings.Contains(rec.Body.String(), "<item>") {
		t.Fatal("aged-out announcements still served")
	}
	again := pollIntoStore(t, store, source, map[string]rfs.Page{source.URL: page})
	if len(again) != len(items) {
		t.Fatal("unchanged poll caused duplicate emissions")
	}
	for i := range items {
		if items[i].GUID != again[i].GUID {
			t.Fatal("unchanged poll changed GUIDs")
		}
	}
}

func TestOtherBusPublicationLabels(t *testing.T) {
	for _, test := range []struct{ id, fixture, label string }{
		{"trieste-trasporti", "triestetrasporti/testdata/avvisi_20260910.html", "Pubblicato: "},
		{"apt-gorizia", "aptgorizia/testdata/avvisi_20260910.html", "Pubblicazione: non disponibile"},
	} {
		t.Run(test.id, func(t *testing.T) {
			store, err := rfs.OpenInMemorySQLiteStore()
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			source := sourceByID(t, test.id)
			pollIntoStore(t, store, source, map[string]rfs.Page{source.URL: fixture(t, test.fixture)})
			handler := rfs.NewHTTPHandlerWithClock(store, []rfs.Source{source}, rfs.BuildInfo{}, feedClock{capturedAt()})
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest("GET", "/feeds/"+test.id+".html", nil))
			if !strings.Contains(rec.Body.String(), `<p class="item-date">`+test.label) {
				t.Fatal(rec.Body.String())
			}
		})
	}
}
