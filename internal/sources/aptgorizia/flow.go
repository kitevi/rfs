// Package aptgorizia watches APT Gorizia's summary of the service changes in
// force.
//
// The page is a standing summary that links each active notice on the
// operator's own site, grouped by service area, so the notice permalink is the
// identity the feed compares. A notice leaving the summary emits nothing;
// disappearance alone does not establish expiry or cancellation.
package aptgorizia

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"golang.org/x/net/html"

	"github.com/ppowo/rfs/internal/rfs"
	"github.com/ppowo/rfs/internal/sources/notices"
)

const (
	// PageURL is the summary page the parser reads.
	PageURL = "https://www.aptgorizia.it/in-evidenza/modifiche-al-servizio-riepilogo-aggiornato-avvisi-attivi-scioperi-variazioni-orari/"
	// HumanURL is the page subscribers should open for context.
	HumanURL = PageURL

	// collectionMarker is the phrase that identifies the summary page, so an
	// error page or a redesigned page fails the poll instead of looking empty.
	collectionMarker = "modifiche al servizio"
)

// noticeCategories are the sections whose notices belong in the feed: service
// notices and route diversions. Corporate news and fares pages do not.
var noticeCategories = []string{"/avvisi-home/", "/deviazioni-di-percorso/"}

// ParseNotices decodes the notices the summary page currently links.
func ParseNotices(page rfs.Page) ([]notices.Notice, error) {
	doc, err := rfs.ParseHTML(page)
	if err != nil {
		return nil, fmt.Errorf("aptgorizia: parse page: %w", err)
	}
	base, err := url.Parse(PageURL)
	if err != nil {
		return nil, fmt.Errorf("aptgorizia: parse page URL: %w", err)
	}
	var list []notices.Notice
	seen := map[string]bool{}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			if notice, ok := anchorNotice(n, base); ok && !seen[notice.ID] {
				seen[notice.ID] = true
				list = append(list, notice)
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	if len(list) == 0 && !strings.Contains(strings.ToLower(string(page)), collectionMarker) {
		return nil, errors.New("aptgorizia: page is neither the summary page nor a page with active notices")
	}
	return list, nil
}

func anchorNotice(anchor *html.Node, base *url.URL) (notices.Notice, bool) {
	href := strings.TrimSpace(notices.Attr(anchor, "href"))
	if href == "" {
		return notices.Notice{}, false
	}
	resolved, err := base.Parse(href)
	if err != nil || resolved.Host != base.Host || !inNoticeCategory(resolved.Path) {
		return notices.Notice{}, false
	}
	title := notices.TextOf(anchor)
	if title == "" {
		return notices.Notice{}, false
	}
	link := resolved.String()
	// The summary page carries no publication date, so none is invented: a
	// notice leaves the summary when it stops applying, and the age cutoff
	// stays out of it rather than being fed a start date as a proxy.
	return notices.Notice{ID: link, Title: title, Link: link, Validity: notices.ValidityFromText(title)}, true
}

func inNoticeCategory(path string) bool {
	for _, category := range noticeCategories {
		if strings.HasPrefix(path, category) {
			return true
		}
	}
	return false
}
