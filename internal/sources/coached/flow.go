// Package coached extracts the All essays catalog from the essay library index.
//
// coached.com publishes no feed. Its /essays/ page lists the writing twice: a
// script-built browser for readers, and a <noscript> fallback the server
// renders for everyone else. The All essays tab is that catalog, and the
// fallback carries the same list under the heading "All essays", so one fetch
// of the index is enough. Newsletter issue pages are a different list — one
// current essay, one older essay, and other links — and this Flow does not
// follow them.
//
// The index gives each essay a title and a link, but no date. Extract leaves
// pubDate unset so rfs stamps the time it first saw the essay
// (docs/adr/0001-current-state-feed.md). The feed stays a projection of the
// essays the page currently lists.
package coached

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/kitevi/rfs/internal/rfs"
)

const (
	// PageURL is the essay library index rfs polls.
	PageURL = "https://coached.com/essays/"

	// HumanURL is the page a reader opens to browse the same essays.
	HumanURL = PageURL

	// ExtractVersion is the derivation version for the Coached essays Flow.
	// Bump it whenever Extract's output can change for a fixed page so rfs
	// forces a full re-derivation instead of trusting a stale HTTP 304.
	ExtractVersion = 1

	// siteHost owns the essay pages the index links to.
	siteHost = "coached.com"

	// guidPrefix makes an essay's slug recoverable from its GUID and keeps
	// identities unique across Sources.
	guidPrefix = "coached:essay:"

	// essayHeading is the fallback heading whose following links are the
	// catalog. The next heading starts the newsletter archive.
	essayHeading = "All essays"
)

// essayPath matches an essay link. A relative path is required, so a link to
// another host cannot be mistaken for an essay. The issues segment is a
// newsletter archive, not an essay slug.
var essayPath = regexp.MustCompile(`^/essays/([a-z0-9-]+)/$`)

type Flow struct{}

// Version reports the Coached essays extraction version.
func (Flow) Version() int { return ExtractVersion }

// Extract turns each essay in the All essays section into one RSS item, in the
// page's order. Repeated links to one essay yield a single item. Items carry
// no pubDate: the index does not date essays.
func (Flow) Extract(page rfs.Page) ([]rfs.ExtractedItem, error) {
	doc, err := rfs.ParseHTML(page)
	if err != nil {
		return nil, fmt.Errorf("coached: parse page: %w", err)
	}
	anchors, err := essaySectionAnchors(doc)
	if err != nil {
		return nil, err
	}

	var items []rfs.ExtractedItem
	seen := make(map[string]bool)
	for _, anchor := range anchors {
		path, slug, ok := essayRef(attr(anchor, "href"))
		if !ok || seen[slug] {
			continue
		}
		title := strings.TrimSpace(textContent(anchor))
		if title == "" {
			continue
		}
		seen[slug] = true
		items = append(items, rfs.ExtractedItem{
			GUID:  guidPrefix + slug,
			Title: title,
			Link:  "https://" + siteHost + path,
		})
	}

	if len(items) == 0 {
		return nil, errors.New("coached: no essays")
	}
	return items, nil
}

// essaySectionAnchors returns the anchors in each <noscript> fallback's All
// essays section. x/net/html keeps noscript markup as a text node rather than
// elements, since a scripting client never renders it, so the fallback has to
// be parsed a second time. Links outside that section — featured cards, and
// the newsletter archive that follows the next heading — are left unread.
func essaySectionAnchors(doc *html.Node) ([]*html.Node, error) {
	var found []*html.Node
	for _, block := range elementNodes(doc, "noscript") {
		fragment, err := html.ParseFragment(strings.NewReader(textContent(block)), &html.Node{
			Type:     html.ElementNode,
			Data:     "body",
			DataAtom: atom.Body,
		})
		if err != nil {
			return nil, fmt.Errorf("coached: parse noscript fallback: %w", err)
		}
		found = append(found, anchorsAfterEssayHeading(fragment)...)
	}
	return found, nil
}

// anchorsAfterEssayHeading returns anchors that follow an "All essays" heading
// and stop at the next heading. Fragment roots are siblings, so the section
// state has to survive from one root to the next.
func anchorsAfterEssayHeading(roots []*html.Node) []*html.Node {
	var found []*html.Node
	inSection := false
	var walk func(*html.Node) bool
	walk = func(node *html.Node) bool {
		if node.Type == html.ElementNode && isHeading(node.Data) {
			if inSection {
				return false
			}
			if strings.TrimSpace(textContent(node)) == essayHeading {
				inSection = true
			}
		}
		if inSection && node.Type == html.ElementNode && node.Data == "a" {
			found = append(found, node)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			if !walk(child) {
				return false
			}
		}
		return true
	}
	for _, root := range roots {
		if !walk(root) {
			break
		}
	}
	return found
}

func isHeading(tag string) bool {
	return len(tag) == 2 && tag[0] == 'h' && tag[1] >= '1' && tag[1] <= '6'
}

// elementNodes returns every element with the given tag name, in document
// order.
func elementNodes(n *html.Node, tag string) []*html.Node {
	var found []*html.Node
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == tag {
			found = append(found, node)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return found
}

// essayRef parses an essay link into its canonical path and slug. The slug
// comes from the path, so an item's identity never depends on its title.
func essayRef(href string) (string, string, bool) {
	trimmed := strings.TrimSpace(href)
	match := essayPath.FindStringSubmatch(trimmed)
	if match == nil || match[1] == "issues" {
		return "", "", false
	}
	return trimmed, match[1], true
}

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

func textContent(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		b.WriteString(textContent(child))
	}
	return b.String()
}
