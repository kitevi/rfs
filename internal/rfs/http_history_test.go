package rfs

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPHandlerServesVisibleHistoryHidingImmatureLive(t *testing.T) {
	store, err := OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	now := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	old := Item{GUID: "ptg:1", Title: "Old", Link: "https://example.com/1", Description: "d", PubDate: now.Add(-48 * time.Hour), Replies: 300}
	live := Item{GUID: "ptg:2", Title: "Live", Link: "https://example.com/2", Description: "d", PubDate: now, Replies: 5}
	if err := store.MergeHistory(t.Context(), "ptg", []Item{old, live}, []string{"ptg:2"}, 11); err != nil {
		t.Fatalf("merge: %v", err)
	}
	handler := NewHTTPHandler(store, []Source{{ID: "ptg", Meta: SourceMeta{Title: "PTG", Description: "d", Link: "https://example.com"}, History: DefaultCatalogHistory()}}, testBuildInfo)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/feeds/ptg.xml", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "ptg:1") {
		t.Fatalf("dead thread missing:\n%s", body)
	}
	if strings.Contains(body, "ptg:2") {
		t.Fatalf("immature live must be hidden:\n%s", body)
	}
}
