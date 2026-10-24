package coached

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kitevi/rfs/internal/rfs"
)

func loadFixture(t *testing.T, name string) rfs.Page {
	t.Helper()
	page, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return page
}

func TestExtractReturnsEveryEssayInPageOrder(t *testing.T) {
	items, err := Flow{}.Extract(loadFixture(t, "essays.html"))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(items) != 47 {
		t.Fatalf("extracted %d items, want 47", len(items))
	}

	first := items[0]
	if first.GUID != "coached:essay:how-to-stop-underselling-yourself" {
		t.Fatalf("first GUID = %q", first.GUID)
	}
	if first.Title != "How to stop underselling yourself" {
		t.Fatalf("first title = %q", first.Title)
	}
	if first.Link != "https://coached.com/essays/how-to-stop-underselling-yourself/" {
		t.Fatalf("first link = %q", first.Link)
	}
	if first.PubDate != nil {
		t.Fatalf("first pubDate = %v, want none", first.PubDate)
	}

	second := items[1]
	if second.GUID != "coached:essay:how-to-negotiate-when-you-hate-negotiating" {
		t.Fatalf("second GUID = %q", second.GUID)
	}

	last := items[len(items)-1]
	if last.GUID != "coached:essay:the-communication-secrets-comedians-use-that-work-in-any-room" {
		t.Fatalf("last GUID = %q", last.GUID)
	}
	if last.Title != "The communication secrets comedians use that work in any room" {
		t.Fatalf("last title = %q", last.Title)
	}

	var decoded bool
	for i, item := range items {
		if item.PubDate != nil {
			t.Fatalf("item %d (%s) has a pubDate", i, item.GUID)
		}
		if item.Description != "" {
			t.Fatalf("item %d (%s) has a description", i, item.GUID)
		}
		if strings.Contains(item.Link, "/essays/issues/") {
			t.Fatalf("item %d link = %q", i, item.Link)
		}
		slug := strings.TrimPrefix(item.GUID, guidPrefix)
		if item.GUID != guidPrefix+slug || item.Link != "https://coached.com/essays/"+slug+"/" {
			t.Fatalf("item %d GUID %q does not name its link %q", i, item.GUID, item.Link)
		}
		if item.Title == "What Tems's rise taught me about careers" {
			decoded = true
		}
	}
	if !decoded {
		t.Fatal("entity in an essay title was not decoded")
	}
}

func TestExtractIgnoresIssuesFeaturedCardsAndForeignLinks(t *testing.T) {
	page := rfs.Page(`<html><body>
<a class="entry-link" href="/essays/only-featured/">Only featured</a>
<a href="https://example.com/essays/from-elsewhere/">Elsewhere</a>
<noscript><section>
<h2>All essays</h2>
<p><a href="/essays/how-to-stop-underselling-yourself/">How to stop underselling yourself</a></p>
<p><a href="https://example.com/essays/how-to-stop-underselling-yourself/">Elsewhere</a></p>
<p><a href="/essays/how-to-stop-underselling-yourself/#top">A fragment</a></p>
<h2>Newsletter issues</h2>
<p><a href="/essays/issues/2026-10-14/">14 Oct 2026 · CS#372: how to brag (yes, really)</a></p>
<p><a href="/essays/hidden-under-issues/">Hidden under issues</a></p>
</section></noscript>
</body></html>`)
	items, err := Flow{}.Extract(page)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("extracted %d items, want 1: %#v", len(items), items)
	}
	if items[0].GUID != "coached:essay:how-to-stop-underselling-yourself" {
		t.Fatalf("GUID = %q", items[0].GUID)
	}
	if items[0].Title != "How to stop underselling yourself" {
		t.Fatalf("title = %q", items[0].Title)
	}
}

func TestExtractDeduplicatesRepeatedEssayLinks(t *testing.T) {
	page := rfs.Page(`<html><body><noscript>
<h2>All essays</h2>
<p><a href="/essays/how-to-stop-underselling-yourself/">How to stop underselling yourself</a></p>
<p><a href="/essays/how-to-stop-underselling-yourself/">Shorter</a></p>
</noscript></body></html>`)
	items, err := Flow{}.Extract(page)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("extracted %d items, want 1: %#v", len(items), items)
	}
	if items[0].Title != "How to stop underselling yourself" {
		t.Fatalf("title = %q, want the first link's text", items[0].Title)
	}
}

func TestExtractRejectsPagesWithoutTheEssaySection(t *testing.T) {
	pages := []rfs.Page{
		rfs.Page(`<html><body><p>Nothing to see here</p></body></html>`),
		rfs.Page(`<html><body>
<a href="/essays/how-to-stop-underselling-yourself/">How to stop underselling yourself</a>
<a href="https://example.com/essays/how-to-stop-underselling-yourself/">Elsewhere</a>
<a href="/essays/issues/2026-10-14/">CS#372: how to brag</a>
<a href="/essays/how-to-stop-underselling-yourself/#top">A fragment</a>
</body></html>`),
		rfs.Page(`<html><body><noscript>
<h2>Newsletter issues</h2>
<p><a href="/essays/issues/2026-10-14/">14 Oct 2026 · CS#372: how to brag (yes, really)</a></p>
<p><a href="/essays/how-to-stop-underselling-yourself/">How to stop underselling yourself</a></p>
</noscript></body></html>`),
	}
	for i, page := range pages {
		if _, err := (Flow{}).Extract(page); err == nil || !strings.Contains(err.Error(), "no essays") {
			t.Fatalf("page %d error = %v, want a rejection", i, err)
		}
	}
}

func TestItemDateURLIsTheEssayLink(t *testing.T) {
	item := rfs.ExtractedItem{GUID: guidPrefix + "you-never-stop-being-nervous", Link: "https://coached.com/essays/you-never-stop-being-nervous/"}
	url, err := Flow{}.ItemDateURL(item)
	if err != nil || url != item.Link {
		t.Fatalf("ItemDateURL = %q, %v; want %q", url, err, item.Link)
	}
	for _, link := range []string{
		"https://example.com/essays/you-never-stop-being-nervous/",
		"https://coached.com/essays/issues/",
		"https://coached.com.example.com/essays/you-never-stop-being-nervous/",
		"",
	} {
		if _, err := (Flow{}).ItemDateURL(rfs.ExtractedItem{GUID: "x", Link: link}); err == nil {
			t.Fatalf("ItemDateURL(%q) succeeded, want an error", link)
		}
	}
}

func TestItemDateReadsPublishedTime(t *testing.T) {
	item := rfs.ExtractedItem{GUID: guidPrefix + "you-never-stop-being-nervous"}
	for content, want := range map[string]time.Time{
		"2026-08-28":                time.Date(2026, 8, 28, 0, 0, 0, 0, time.UTC),
		"2026-08-28T09:30:00+02:00": time.Date(2026, 8, 28, 7, 30, 0, 0, time.UTC),
	} {
		page := rfs.Page(`<html><head><meta property="og:type" content="article"><meta property="article:published_time" content="` + content + `"></head><body></body></html>`)
		date, err := Flow{}.ItemDate(page, item)
		if err != nil {
			t.Fatalf("ItemDate(%q): %v", content, err)
		}
		if !date.Equal(want) || date.Location() != time.UTC {
			t.Fatalf("ItemDate(%q) = %v, want %v", content, date, want)
		}
	}
}

func TestItemDateRejectsPagesWithoutAReadableDate(t *testing.T) {
	item := rfs.ExtractedItem{GUID: guidPrefix + "you-never-stop-being-nervous"}
	for _, page := range []rfs.Page{
		rfs.Page(`<html><head><title>No date</title></head></html>`),
		rfs.Page(`<html><head><meta property="article:published_time" content="August 28"></head></html>`),
		rfs.Page(``),
	} {
		if _, err := (Flow{}).ItemDate(page, item); err == nil {
			t.Fatalf("ItemDate(%q) succeeded, want an error", page)
		}
	}
}
