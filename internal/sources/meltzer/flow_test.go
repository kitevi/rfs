package meltzer_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/html"

	"github.com/kitevi/rfs/internal/rfs"

	"github.com/kitevi/rfs/internal/sources/meltzer"
)

func TestFlowExtractsMatchRowsAsItems(t *testing.T) {
	doc := parseHTML(t, `
<html><body>
<h3>2020s</h3>
<table class="wikitable">
  <tr>
    <th>Date</th><th>Match</th><th>Promotion</th><th>Event</th><th>Rating</th><th>Notes</th><th>Ref.</th>
  </tr>
  <tr>
    <td>April 7, 2021</td>
    <td><a href="/wiki/Ric_Flair">Ric Flair</a> vs. <a href="/wiki/Butch_Reed">Butch Reed</a><sup class="reference">[a]</sup></td>
    <td><a href="/wiki/Championship_Wrestling_from_Florida">CWF</a></td>
    <td>Miami Beach show</td>
    <td>5</td>
    <td>for the NWA World Heavyweight Championship</td>
    <td>[2]</td>
  </tr>
</table>
</body></html>`)

	items, err := (meltzer.Flow{}).Extract(doc)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d: %#v", len(items), items)
	}

	item := items[0]
	if item.GUID != "meltzer:2021-04-07:ric-flair-vs-butch-reed:miami-beach-show" {
		t.Fatalf("unexpected GUID: %q", item.GUID)
	}
	if item.Title != "Ric Flair vs. Butch Reed — Miami Beach show" {
		t.Fatalf("unexpected title: %q", item.Title)
	}
	if item.Link != meltzer.PageURL {
		t.Fatalf("unexpected link: %q", item.Link)
	}
	if item.Description != "CWF · 5 stars · for the NWA World Heavyweight Championship" {
		t.Fatalf("unexpected description: %q", item.Description)
	}
	if item.PubDate == nil {
		t.Fatal("expected source-derived pubDate")
	}
	wantDate := time.Date(2021, 4, 7, 0, 0, 0, 0, time.UTC)
	if !item.PubDate.Equal(wantDate) {
		t.Fatalf("unexpected pubDate: got %s want %s", item.PubDate, wantDate)
	}
}

func TestFlowHandlesWikipediaRowspanDates(t *testing.T) {
	doc := parseHTML(t, `
<html><body>
<h3>2020s</h3>
<table class="wikitable sortable">
  <tr>
    <th></th><th></th><th>Date</th><th>Match</th><th>Promotion</th><th>Event</th><th>Rating</th><th>Notes</th><th>Ref.</th>
  </tr>
  <tr>
    <td>162</td><td>64</td><td rowspan="2">August 31, 2020</td>
    <td>Walter vs. Tyler Bate</td><td>WWE</td><td>NXT UK TakeOver: Cardiff</td><td>5.25</td><td>for the WWE United Kingdom Championship</td><td>163</td>
  </tr>
  <tr>
    <td>163</td><td>65</td>
    <td>Lucha Brothers (Pentagón Jr. and Rey Fenix) vs. The Young Bucks (Matt Jackson and Nick Jackson)</td><td>AEW</td><td>All Out</td><td>5.25</td><td>Ladder match for the AAA World Tag Team Championship</td><td>164</td>
  </tr>
</table>
</body></html>`)

	items, err := (meltzer.Flow{}).Extract(doc)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d: %#v", len(items), items)
	}

	second := items[1]
	if second.Title != "Lucha Brothers (Pentagón Jr. and Rey Fenix) vs. The Young Bucks (Matt Jackson and Nick Jackson) — All Out" {
		t.Fatalf("rowspan row shifted columns, title = %q", second.Title)
	}
	if second.Description != "AEW · 5.25 stars · Ladder match for the AAA World Tag Team Championship" {
		t.Fatalf("rowspan row shifted columns, description = %q", second.Description)
	}
	wantDate := time.Date(2020, 8, 31, 0, 0, 0, 0, time.UTC)
	if second.PubDate == nil || !second.PubDate.Equal(wantDate) {
		t.Fatalf("rowspan date not carried forward: %#v", second.PubDate)
	}
}

func TestFlowIgnoresOtherDecadeTables(t *testing.T) {
	doc := parseHTML(t, `
<html><body>
<h3>2010s</h3>
<table class="wikitable">
  <tr><th>Date</th><th>Match</th><th>Promotion</th><th>Event</th><th>Rating</th><th>Notes</th><th>Ref.</th></tr>
  <tr><td>August 31, 2019</td><td>Walter vs. Tyler Bate</td><td>WWE</td><td>NXT UK</td><td>5.25</td><td>UK title</td><td>1</td></tr>
</table>
<h3>2020s</h3>
<table class="wikitable">
  <tr><th>Date</th><th>Match</th><th>Promotion</th><th>Event</th><th>Rating</th><th>Notes</th><th>Ref.</th></tr>
  <tr><td>January 4, 2020</td><td>Will Ospreay vs. Hiromu Takahashi</td><td>NJPW</td><td>Wrestle Kingdom 14</td><td>5.5</td><td>Jr title</td><td>1</td></tr>
</table>
</body></html>`)

	items, err := (meltzer.Flow{}).Extract(doc)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected only the 2020s item, got %d: %#v", len(items), items)
	}
	if !strings.Contains(items[0].Title, "Will Ospreay") {
		t.Fatalf("expected the 2020s item, got %q", items[0].Title)
	}
}

// TestFlowParsesEvery2020sFixtureRow verifies the real 2020s section from
// Wikipedia (testdata/2020s.html) parses end to end with no dropped rows.
func TestFlowParsesEvery2020sFixtureRow(t *testing.T) {
	raw, err := os.ReadFile("testdata/2020s.html")
	if err != nil {
		t.Fatalf("read testdata/2020s.html: %v", err)
	}
	doc := parseHTML(t, string(raw))

	items, err := (meltzer.Flow{}).Extract(doc)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}

	wantRows := countDataRows(t, doc)
	if len(items) != wantRows {
		t.Fatalf("expected every 2020s data row to produce an item; got %d items for %d rows", len(items), wantRows)
	}

	containsTitle := func(substr string) bool {
		for _, it := range items {
			if strings.Contains(it.Title, substr) {
				return true
			}
		}
		return false
	}

	// First row: Wrestle Kingdom 14 Night 1, the <small> "Night 1" suffix is
	// flattened into the title.
	first := items[0]
	if !strings.Contains(first.Title, "Will Ospreay vs. Hiromu Takahashi") {
		t.Fatalf("unexpected first title: %q", first.Title)
	}
	if !strings.Contains(first.Title, "Wrestle Kingdom 14 Night 1") {
		t.Fatalf("expected Night 1 suffix flattened into title: %q", first.Title)
	}
	if !strings.Contains(first.Description, "NJPW") || !strings.Contains(first.Description, "5.5 stars") {
		t.Fatalf("unexpected first description: %q", first.Description)
	}
	wantFirst := time.Date(2020, 1, 4, 0, 0, 0, 0, time.UTC)
	if first.PubDate == nil || !first.PubDate.Equal(wantFirst) {
		t.Fatalf("unexpected first pubDate: %#v", first.PubDate)
	}

	// Second row carries the rowspan date from the first.
	second := items[1]
	if !strings.Contains(second.Title, "Kazuchika Okada vs. Kota Ibushi") {
		t.Fatalf("unexpected second title: %q", second.Title)
	}
	if second.PubDate == nil || !second.PubDate.Equal(wantFirst) {
		t.Fatalf("rowspan date not carried to second row: %#v", second.PubDate)
	}

	// <small> member lists are flattened into match titles cleanly, with no
	// stray spacing inside the parentheses.
	if !containsTitle("Young Bucks (Matt Jackson and Nick Jackson)") {
		t.Fatalf("expected a flattened <small> member list in a title")
	}

	// Last row of the snapshot.
	last := items[len(items)-1]
	if !strings.Contains(last.Title, "Will Ospreay vs. Swerve Strickland") || !strings.Contains(last.Title, "Forbidden Door") {
		t.Fatalf("unexpected last title: %q", last.Title)
	}
	if !strings.Contains(last.Description, "5.5 stars") || !strings.Contains(last.Description, "Owen Hart Cup final") {
		t.Fatalf("unexpected last description: %q", last.Description)
	}
	wantLast := time.Date(2026, 6, 28, 0, 0, 0, 0, time.UTC)
	if last.PubDate == nil || !last.PubDate.Equal(wantLast) {
		t.Fatalf("unexpected last pubDate: %#v", last.PubDate)
	}
}

func parseHTML(t *testing.T, body string) rfs.Page {
	t.Helper()
	page := rfs.Page(body)
	if _, err := rfs.ParseHTML(page); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return page
}

func countDataRows(t *testing.T, page rfs.Page) int {
	t.Helper()
	doc, err := rfs.ParseHTML(page)
	if err != nil {
		t.Fatalf("parse fixture for row count: %v", err)
	}
	table := firstTable(doc)
	if table == nil {
		t.Fatal("fixture has no table element")
	}
	count := 0
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "tr" && hasDirectChild(n, "td") {
			count++
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(table)
	return count
}

func firstTable(doc *html.Node) *html.Node {
	var found *html.Node
	var walk func(*html.Node) bool
	walk = func(n *html.Node) bool {
		if n.Type == html.ElementNode && n.Data == "table" {
			found = n
			return true
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if walk(c) {
				return true
			}
		}
		return false
	}
	walk(doc)
	return found
}

func hasDirectChild(n *html.Node, tag string) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.Data == tag {
			return true
		}
	}
	return false
}
