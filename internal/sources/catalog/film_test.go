package catalog_test

import (
	"testing"
	"time"

	"github.com/kitevi/rfs/internal/rfs"
	"github.com/kitevi/rfs/internal/sources/catalog"
)

func TestFilmFlowEmitsLiveThread(t *testing.T) {
	page := rfs.Page(`[{"page":1,"threads":[{"no":222965645,"sub":"/film/","com":"Arthouse &amp; Classics<br><br>Guiltydition","time":1788463410}]}]`)

	items, err := catalog.Film.Extract(page)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 live thread, got %d: %#v", len(items), items)
	}
	item := items[0]
	if item.GUID != "film:222965645" {
		t.Fatalf("unexpected GUID: %q", item.GUID)
	}
	if item.Link != "https://boards.4chan.org/tv/thread/222965645/" {
		t.Fatalf("unexpected link: %q", item.Link)
	}
	if item.Title != "Arthouse & Classics" {
		t.Fatalf("unexpected title: %q", item.Title)
	}
	wantDate := time.Unix(1788463410, 0).UTC()
	if item.PubDate == nil || !item.PubDate.Equal(wantDate) {
		t.Fatalf("unexpected pubDate: %#v, want %v", item.PubDate, wantDate)
	}
	if item.Replies != 0 {
		t.Fatalf("unexpected replies without count: %d", item.Replies)
	}
}

func TestFilmFlowParsesReplyCounts(t *testing.T) {
	page := rfs.Page(`[{"page":1,"threads":[{"no":222965645,"sub":"/film/","com":"Edition","time":1788463410,"replies":120,"images":10}]}]`)
	items, err := catalog.Film.Extract(page)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Replies != 120 {
		t.Fatalf("replies = %d, want 120", items[0].Replies)
	}
}

func TestFilmFlowEmitsAllMatchingThreadsAndIgnoresOthers(t *testing.T) {
	page := rfs.Page(`[{"page":1,"threads":[{"no":111,"sub":"/tv/ general","com":"other stuff","time":1788460000},{"no":222965645,"sub":"/film/","com":"first Edition","time":1788463410},{"no":222970000,"sub":"/FILM/ follow-up","com":"second Edition","time":1788500000},{"no":999,"com":"sticky without subject","time":1788460000}]}]`)

	items, err := catalog.Film.Extract(page)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 film threads (no live-drop), got %d: %#v", len(items), items)
	}
	if items[0].GUID != "film:222965645" || items[1].GUID != "film:222970000" {
		t.Fatalf("unexpected GUIDs: %q, %q", items[0].GUID, items[1].GUID)
	}
}

func TestFilmFlowSkipsInvalidThreads(t *testing.T) {
	page := rfs.Page(`[{"page":1,"threads":[{"no":222965645,"sub":"/film/","com":"valid Edition","time":1788463410},{"no":0,"sub":"/film/ bad no","com":"x","time":1788463410},{"no":222965646,"sub":"/film/ no com","time":1788463410},{"no":222965647,"sub":"/film/ no time","com":"x"},{"no":222965648,"sub":"/film/ reply","com":"x","time":1788463410,"resto":222965645}]}]`)

	items, err := catalog.Film.Extract(page)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if len(items) != 1 || items[0].GUID != "film:222965645" {
		t.Fatalf("expected only the valid thread, got %#v", items)
	}
}

func TestFilmFlowErrorsWhenNoMatch(t *testing.T) {
	page := rfs.Page(`[{"page":1,"threads":[{"no":111,"sub":"/tv/ general","com":"x","time":1788460000}]}]`)

	if _, err := catalog.Film.Extract(page); err == nil {
		t.Fatal("expected an error when no thread matches /film/")
	}
}

func TestFilmFlowErrorsWhenOnlyInvalidMatchesRemain(t *testing.T) {
	page := rfs.Page(`[{"page":1,"threads":[{"no":222965645,"sub":"/film/","time":1788463410}]}]`)

	if _, err := catalog.Film.Extract(page); err == nil {
		t.Fatal("expected an error when matches exist but none are valid")
	}
}

func TestFilmFlowDecodesComFragment(t *testing.T) {
	page := rfs.Page(`[{"page":1,"threads":[{"no":221573858,"sub":"/film/","com":"Thread for the discussion of arthouse and classic cinema.<br><br>Don Carlo edition<br><span class=\"quote\">&gt;QOTD</span><br>What did you watch this week? <a href=\"https://example.com/chart\">chart</a>","time":1788463410}]}]`)

	items, err := catalog.Film.Extract(page)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	wantDesc := "Thread for the discussion of arthouse and classic cinema.\n\nDon Carlo edition\n>QOTD\nWhat did you watch this week? chart"
	if items[0].Description != wantDesc {
		t.Fatalf("unexpected description: %q, want %q", items[0].Description, wantDesc)
	}
	if items[0].Title != "Thread for the discussion of arthouse and classic cinema." {
		t.Fatalf("unexpected title: %q", items[0].Title)
	}
}

func TestFilmFlowRejectsInvalidJSON(t *testing.T) {
	for _, body := range []string{`not json`, `{}`, `[]`} {
		if _, err := catalog.Film.Extract(rfs.Page(body)); err == nil {
			t.Fatalf("expected an error for %q", body)
		}
	}
}

func TestFilmRebuildStripsLegacyPrefix(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"/film/ - Arthouse & Classic Cinema \u2014 noir edition", "Arthouse & Classic Cinema \u2014 noir edition"},
		{"/film/ - Arthouse & Classic Cinema", "Arthouse & Classic Cinema"},
		{"Arthouse & Classic Cinema \u2014 noir edition", "Arthouse & Classic Cinema \u2014 noir edition"},
	} {
		if got, err := catalog.Film.RebuildStored(rfs.Item{Title: tc.in}); err != nil || got.Title != tc.want {
			t.Fatalf("RebuildStored(%q) = %q, want %q", tc.in, got.Title, tc.want)
		}
	}
}

func TestFilmFlowVersion(t *testing.T) {
	if catalog.Film.Version() != catalog.Film.ExtractVersion {
		t.Fatalf("Version() = %d, want %d", catalog.Film.Version(), catalog.Film.ExtractVersion)
	}
	if catalog.Film.ExtractVersion != 5 {
		t.Fatalf("ExtractVersion = %d, want 5 (history rebuild must replay over stored rows)", catalog.Film.ExtractVersion)
	}
}
