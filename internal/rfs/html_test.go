package rfs

import (
	"strings"
	"testing"
)

func TestRenderHTMLFeedShowsObservationTrace(t *testing.T) {
	body, err := RenderHTMLFeed("acloserlisten", SourceMeta{Title: "A Closer Listen recommendations"}, nil, "H1 skipped iframe: <not a player>", BuildInfo{})
	if err != nil {
		t.Fatal(err)
	}
	page := string(body)
	if !strings.Contains(page, "Latest observation") {
		t.Fatalf("page has no observation heading:\n%s", page)
	}
	if !strings.Contains(page, "H1 skipped iframe: &lt;not a player&gt;") {
		t.Fatalf("trace was missing or not escaped:\n%s", page)
	}
}

func TestRenderHTMLFeedOmitsEmptyObservation(t *testing.T) {
	body, err := RenderHTMLFeed("acloserlisten", SourceMeta{Title: "A Closer Listen recommendations"}, nil, "", BuildInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), "Latest observation") {
		t.Fatal("empty trace rendered an observation section")
	}
}
