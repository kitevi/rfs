// Package triestetrasporti watches Trieste Trasporti's service-notice page.
//
// The page lists the notices in force as cards and keeps the expired ones in
// an archive section below an anchor. Each card carries its own permalink, so a
// single fetch is enough to publish the list and the permalink is the identity
// the feed compares.
package triestetrasporti

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/ppowo/rfs/internal/rfs"
	"github.com/ppowo/rfs/internal/sources/notices"
)

const (
	// PageURL is the collection page the parser reads.
	PageURL = "https://www.triestetrasporti.it/it/avvisi-infomobilita"
	// HumanURL is the page subscribers should open for context.
	HumanURL = PageURL

	// archiveMarkerID starts the section that keeps expired notices. It is
	// required: without it the parser cannot tell an expired card from a notice
	// in force, and publishing one as new would be worse than failing the poll.
	archiveMarkerID = "archivio-avvisi"
)

// ParseNotices decodes the notices in force, dropping the archived ones.
func ParseNotices(page rfs.Page) ([]notices.Notice, error) {
	doc, err := rfs.ParseHTML(page)
	if err != nil {
		return nil, fmt.Errorf("triestetrasporti: parse page: %w", err)
	}
	base, err := url.Parse(PageURL)
	if err != nil {
		return nil, fmt.Errorf("triestetrasporti: parse page URL: %w", err)
	}
	var list []notices.Notice
	markerSeen, archived := false, false
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if notices.Attr(n, "id") == archiveMarkerID {
				markerSeen, archived = true, true
				return
			}
			if !archived && n.Data == "div" && notices.HasClass(n, "card-square") {
				if notice, ok := cardNotice(n, base); ok {
					list = append(list, notice)
				}
				return
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	if !markerSeen {
		return nil, errors.New("triestetrasporti: page has no archive marker, so the notices in force cannot be told apart from expired ones")
	}
	return list, nil
}

// cardNotice reads one card. A card without a permalink or a headline is not a
// notice: the theme also uses cards for navigation links.
func cardNotice(card *html.Node, base *url.URL) (notices.Notice, bool) {
	href := ""
	for _, anchor := range anchors(card) {
		candidate := strings.TrimSpace(notices.Attr(anchor, "href"))
		if candidate == "" || strings.HasPrefix(candidate, "#") {
			continue
		}
		if resolved, err := base.Parse(candidate); err == nil {
			href = resolved.String()
			break
		}
	}
	title := notices.TextOf(notices.FindByClass(card, "div", "title"))
	if href == "" || title == "" {
		return notices.Notice{}, false
	}
	summary := notices.TextOf(notices.FindByClass(card, "div", "description"))
	return notices.Notice{
		ID:        href,
		Title:     title,
		Summary:   summary,
		Link:      href,
		Published: cardDate(card),
		Validity:  notices.ValidityFromText(title + " " + summary),
	}, true
}

func anchors(root *html.Node) []*html.Node {
	var found []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			found = append(found, n)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return found
}

// cardDate reads the date the card shows for its notice. The theme writes it as
// a wall-clock reading with a Z suffix, so the calendar day is taken as written
// instead of being shifted into the zone the suffix claims.
func cardDate(card *html.Node) notices.Date {
	stamp := notices.Attr(notices.FindFirst(card, "time"), "datetime")
	if stamp == "" {
		return notices.Date{}
	}
	parsed, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		return notices.Date{Text: stamp}
	}
	return notices.DayDate(time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 0, 0, 0, 0, notices.Local()))
}
