package meltzer

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/html"

	"github.com/kitevi/rfs/internal/rfs"
)

const PageURL = "https://en.wikipedia.org/wiki/List_of_professional_wrestling_matches_rated_5_or_more_stars_by_Dave_Meltzer"

// targetDecade is the only decade section parsed from the Meltzer page.
const targetDecade = "2020s"

// ExtractVersion is the derivation version for the Meltzer Flow. Bump it
// whenever Extract/textContent's output can change for a fixed page so rfs
// forces a full re-derivation instead of trusting a stale HTTP 304.
const ExtractVersion = 1

type Flow struct{}

// Version reports the Meltzer extraction version.
func (Flow) Version() int { return ExtractVersion }

func (Flow) Extract(page rfs.Page) ([]rfs.ExtractedItem, error) {
	doc, err := rfs.ParseHTML(page)
	if err != nil {
		return nil, fmt.Errorf("meltzer: parse page: %w", err)
	}

	var items []rfs.ExtractedItem

	// Walk the document in order, remembering the most recent heading so each
	// table can be attributed to the decade it appears under. Only the 2020s
	// table is parsed; every other decade is skipped.
	heading := ""
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "h1", "h2", "h3", "h4", "h5", "h6":
				heading = headingText(textContent(n))
			case "table":
				if isTargetDecade(heading) {
					items = append(items, extractTable(n)...)
				}
				return // never descend into tables
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)

	return items, nil
}

// extractTable parses a single decade table into extracted items.
func extractTable(table *html.Node) []rfs.ExtractedItem {
	rows := findElements(table, "tr")
	if len(rows) < 2 {
		return nil
	}

	headers, columnCount := headerIndex(rows[0])
	if !hasHeaders(headers, "date", "match", "promotion", "event", "rating") {
		return nil
	}

	spans := map[int]carriedCell{}
	var items []rfs.ExtractedItem
	for _, row := range rows[1:] {
		cells := childElements(row, "td")
		if len(cells) == 0 {
			continue
		}
		alignedCells := alignCells(cells, columnCount, spans)

		cell := func(name string) string {
			idx, ok := headers[name]
			if !ok || idx >= len(alignedCells) || alignedCells[idx] == nil {
				return ""
			}
			return textContent(alignedCells[idx])
		}

		dateText := cell("date")
		match := cell("match")
		promotion := cell("promotion")
		event := cell("event")
		rating := cell("rating")
		notes := cell("notes")

		if match == "" || event == "" {
			continue
		}

		var pubDate *time.Time
		datePart := "no-date"
		if dateText != "" {
			datePart = slug(dateText)
			parsed, err := parseMeltzerDate(dateText)
			if err == nil {
				pubDate = &parsed
				datePart = parsed.Format("2006-01-02")
			}
		}

		guid := "meltzer:" + datePart + ":" + slug(match) + ":" + slug(event)
		description := strings.Join(nonEmpty([]string{promotion, starText(rating), notes}), " · ")

		items = append(items, rfs.ExtractedItem{
			GUID:        guid,
			Title:       match + " — " + event,
			Link:        PageURL,
			Description: description,
			PubDate:     pubDate,
		})
	}
	return items
}

// headingText normalizes a heading's text for decade comparison.
func headingText(raw string) string {
	return strings.ToLower(strings.Join(strings.Fields(raw), " "))
}

// isTargetDecade reports whether heading is the 2020s decade. HasPrefix
// tolerates legacy MediaWiki headings that append an "[edit]" section link.
func isTargetDecade(heading string) bool {
	return strings.HasPrefix(strings.TrimSpace(heading), targetDecade)
}

func headerIndex(row *html.Node) (map[string]int, int) {
	cells := childElements(row, "th")
	headers := map[string]int{}
	for i, cell := range cells {
		name := strings.ToLower(textContent(cell))
		if name != "" {
			headers[name] = i
		}
	}
	return headers, len(cells)
}

type carriedCell struct {
	node     *html.Node
	rowsLeft int
}

func alignCells(cells []*html.Node, columnCount int, spans map[int]carriedCell) []*html.Node {
	aligned := make([]*html.Node, columnCount)
	cellIndex := 0
	for column := 0; column < columnCount; column++ {
		if carried, ok := spans[column]; ok {
			aligned[column] = carried.node
			carried.rowsLeft--
			if carried.rowsLeft <= 0 {
				delete(spans, column)
			} else {
				spans[column] = carried
			}
			continue
		}

		if cellIndex >= len(cells) {
			continue
		}
		cell := cells[cellIndex]
		cellIndex++
		aligned[column] = cell
		if rows := intAttr(cell, "rowspan"); rows > 1 {
			spans[column] = carriedCell{node: cell, rowsLeft: rows - 1}
		}
	}
	return aligned
}

func intAttr(n *html.Node, name string) int {
	for _, attr := range n.Attr {
		if attr.Key != name {
			continue
		}
		value, err := strconv.Atoi(attr.Val)
		if err != nil {
			return 0
		}
		return value
	}
	return 0
}

func hasHeaders(headers map[string]int, names ...string) bool {
	for _, name := range names {
		if _, ok := headers[name]; !ok {
			return false
		}
	}
	return true
}

func parseMeltzerDate(value string) (time.Time, error) {
	return time.ParseInLocation("January 2, 2006", value, time.UTC)
}

func starText(rating string) string {
	if rating == "" {
		return ""
	}
	if strings.Contains(strings.ToLower(rating), "star") {
		return rating
	}
	return rating + " stars"
}

func nonEmpty(values []string) []string {
	out := values[:0]
	for _, value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func slug(value string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(value) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func findElements(root *html.Node, name string) []*html.Node {
	var out []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == name {
			out = append(out, n)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return out
}

func childElements(root *html.Node, name string) []*html.Node {
	var out []*html.Node
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == name {
			out = append(out, child)
		}
	}
	return out
}

func textContent(root *html.Node) string {
	var parts []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "sup" || hasClass(n, "reference")) {
			return
		}
		if n.Type == html.TextNode {
			parts = append(parts, n.Data)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	// Parsoid emits each wikitext link as its own text node, so naively
	// joining them leaves stray spaces next to commas and parentheses in
	// tag-team names like "FTR ( Matt Jackson and Nick Jackson )". Join with
	// Fields first to collapse runs of whitespace, then tidy the punctuation.
	s := strings.Join(strings.Fields(strings.Join(parts, " ")), " ")
	s = strings.ReplaceAll(s, " ,", ",")
	s = strings.ReplaceAll(s, "( ", "(")
	s = strings.ReplaceAll(s, " )", ")")
	return s
}

func hasClass(n *html.Node, class string) bool {
	for _, attr := range n.Attr {
		if attr.Key != "class" {
			continue
		}
		for _, part := range strings.Fields(attr.Val) {
			if part == class {
				return true
			}
		}
	}
	return false
}
