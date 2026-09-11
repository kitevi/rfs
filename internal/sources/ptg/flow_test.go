package ptg_test

import (
	"testing"
	"time"

	"github.com/kitevi/rfs/internal/rfs"
	"github.com/kitevi/rfs/internal/sources/ptg"
)

func TestFlowEmitsLiveThread(t *testing.T) {
	page := rfs.Page(`[{"page":1,"threads":[{"no":109697201,"sub":"/ptg/ - Private Trackers General","com":"the tummies remain private Edition<br>FAQ: https://example.com","time":1788200342}]}]`)

	items, err := (ptg.Flow{}).Extract(page)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 live thread, got %d: %#v", len(items), items)
	}
	item := items[0]
	if item.GUID != "ptg:109697201" {
		t.Fatalf("unexpected GUID: %q", item.GUID)
	}
	if item.Link != "https://boards.4chan.org/g/thread/109697201/" {
		t.Fatalf("unexpected link: %q", item.Link)
	}
	if item.Title != "Private Trackers General \u2014 the tummies remain private Edition" {
		t.Fatalf("unexpected title: %q", item.Title)
	}
	wantDate := time.Unix(1788200342, 0).UTC()
	if item.PubDate == nil || !item.PubDate.Equal(wantDate) {
		t.Fatalf("unexpected pubDate: %#v, want %v", item.PubDate, wantDate)
	}
	if item.Replies != 0 {
		t.Fatalf("unexpected replies without count: %d", item.Replies)
	}
}

func TestFlowParsesReplyCounts(t *testing.T) {
	page := rfs.Page(`[{"page":1,"threads":[{"no":109697201,"sub":"/ptg/ - Private Trackers General","com":"Edition","time":1788200342,"replies":150,"images":20},{"no":109730000,"sub":"/ptg/ next","com":"Edition","time":1788300000}]}]`)
	items, err := (ptg.Flow{}).Extract(page)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].Replies != 150 {
		t.Fatalf("replies = %d, want 150", items[0].Replies)
	}
	if items[1].Replies != 0 {
		t.Fatalf("missing replies should default to 0, got %d", items[1].Replies)
	}
}

func TestFlowEmitsAllMatchingThreadsAndIgnoresOthers(t *testing.T) {
	page := rfs.Page(`[{"page":1,"threads":[{"no":1,"sub":"/lmg/ - Local Models General","com":"local stuff","time":1788200000},{"no":109697201,"sub":"/ptg/ - Private Trackers General","com":"first Edition","time":1788200342},{"no":109730000,"sub":"/PTG/ follow-up","com":"second Edition","time":1788300000},{"no":999,"com":"sticky without subject","time":1788200000}]}]`)

	items, err := (ptg.Flow{}).Extract(page)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 ptg threads (no live-drop), got %d: %#v", len(items), items)
	}
	if items[0].GUID != "ptg:109697201" || items[1].GUID != "ptg:109730000" {
		t.Fatalf("unexpected GUIDs: %q, %q", items[0].GUID, items[1].GUID)
	}
}

func TestFlowSkipsInvalidThreads(t *testing.T) {
	page := rfs.Page(`[{"page":1,"threads":[{"no":109697201,"sub":"/ptg/ - Private Trackers General","com":"valid Edition","time":1788200342},{"no":0,"sub":"/ptg/ bad no","com":"x","time":1788200342},{"no":109697202,"sub":"/ptg/ no com","time":1788200342},{"no":109697203,"sub":"/ptg/ no time","com":"x"},{"no":109697204,"sub":"/ptg/ reply","com":"x","time":1788200342,"resto":109697201}]}]`)

	items, err := (ptg.Flow{}).Extract(page)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if len(items) != 1 || items[0].GUID != "ptg:109697201" {
		t.Fatalf("expected only the valid thread, got %#v", items)
	}
}

func TestFlowErrorsWhenNoMatch(t *testing.T) {
	page := rfs.Page(`[{"page":1,"threads":[{"no":1,"sub":"/lmg/ - Local Models General","com":"x","time":1788200000}]}]`)

	if _, err := (ptg.Flow{}).Extract(page); err == nil {
		t.Fatal("expected an error when no thread matches /ptg/")
	}
}

func TestFlowErrorsWhenOnlyInvalidMatchesRemain(t *testing.T) {
	page := rfs.Page(`[{"page":1,"threads":[{"no":109697201,"sub":"/ptg/ - Private Trackers General","time":1788200342}]}]`)

	if _, err := (ptg.Flow{}).Extract(page); err == nil {
		t.Fatal("expected an error when matches exist but none are valid")
	}
}

func TestFlowDecodesComFragment(t *testing.T) {
	page := rfs.Page(`[{"page":1,"threads":[{"no":109697201,"sub":"/ptg/ - Private Trackers General","com":"RED edition<br><br><span class=\"quote\">&gt;Not sure what private trackers are?</span><br>A private tracker is an invite-only site. FAQ<wbr>_link &amp; friends <a href=\"//boards.4chan.org/g/catalog#s=ptg\" class=\"quotelink\">&gt;&gt;&gt;/g/ptg</a>","time":1788200342}]}]`)

	items, err := (ptg.Flow{}).Extract(page)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	wantDesc := "RED edition\n\n>Not sure what private trackers are?\nA private tracker is an invite-only site. FAQ_link & friends >>>/g/ptg"
	if items[0].Description != wantDesc {
		t.Fatalf("unexpected description: %q, want %q", items[0].Description, wantDesc)
	}
	if items[0].Title != "Private Trackers General \u2014 RED edition" {
		t.Fatalf("unexpected title: %q", items[0].Title)
	}
}

func TestFlowRejectsInvalidJSON(t *testing.T) {
	for _, body := range []string{`not json`, `{}`, `[]`} {
		if _, err := (ptg.Flow{}).Extract(rfs.Page(body)); err == nil {
			t.Fatalf("expected an error for %q", body)
		}
	}
}

func TestFlowVersion(t *testing.T) {
	if (ptg.Flow{}).Version() != ptg.ExtractVersion {
		t.Fatalf("Version() = %d, want %d", (ptg.Flow{}).Version(), ptg.ExtractVersion)
	}
	if ptg.ExtractVersion != 5 {
		t.Fatalf("ExtractVersion = %d, want 5 (title stripping must invalidate stored snapshots)", ptg.ExtractVersion)
	}
}
