package osmer

import (
	"errors"
	"fmt"
	htmlstdlib "html"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/ppowo/rfs/internal/rfs"
)

const (
	// PageURL is the official ARPA FVG OSMER bulletin for coastal zone Z4,
	// the forecast zone that includes Trieste.
	PageURL        = "https://www.meteo.fvg.it/previsioni.php?dettaglio=Z4&ln=it"
	ExtractVersion = 1
)

// Flow extracts coastal-zone OSMER forecast sections that explicitly mention
// rain-forming precipitation. OSMER updates each section asynchronously
// through the day, so the forecast date and source-owned issue timestamp form
// a stable GUID.
type Flow struct{}

// Version reports the OSMER extraction version.
func (Flow) Version() int { return ExtractVersion }

var (
	issueRe = regexp.MustCompile(`emissione:\s*(\d{2})-(\d{2})-(\d{4}) (\d{2}):(\d{2}) (CET|CEST)`)
	rainRe  = regexp.MustCompile(`(?i)\b(pioggia|piogge|pioviggina|pioviggini|precipitazione|precipitazioni|rovescio|rovesci|temporale|temporali)\b`)
)

// Extract emits one RSS item for each forecast section whose prose mentions
// measurable precipitation relevant to Trieste's coastal area. The section can
// be oggi, domani, dopodomani, or one of the following tendenza sections.
func (Flow) Extract(page rfs.Page) ([]rfs.ExtractedItem, error) {
	doc, err := rfs.ParseHTML(page)
	if err != nil {
		return nil, fmt.Errorf("osmer rain: parse page: %w", err)
	}

	var items []rfs.ExtractedItem
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "panel-body") {
			if item, ok := extractSection(n); ok {
				items = append(items, item)
			}
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)

	if len(items) == 0 {
		return nil, errors.New("osmer rain: no rain-bearing forecast sections found")
	}
	return items, nil
}

func extractSection(section *html.Node) (rfs.ExtractedItem, bool) {
	description := normalizeSpace(textContent(findByClass(section, "text-justify", "sipadd")))
	if !rainRe.MatchString(description) {
		return rfs.ExtractedItem{}, false
	}

	labelNode := findByAttr(section, "strong", "", "")
	issueNode := findEmissione(section)
	if labelNode == nil || issueNode == nil {
		return rfs.ExtractedItem{}, false
	}

	label := normalizeSpace(textContent(labelNode))
	issuedAt, err := parseIssueTime(textContent(issueNode))
	if err != nil || label == "" || description == "" {
		return rfs.ExtractedItem{}, false
	}
	forecastDate, err := forecastDate(label, issuedAt)
	if err != nil {
		return rfs.ExtractedItem{}, false
	}
	if summary := probabilitySummary(section); summary != "" {
		description += " " + summary
	}

	title := "Rain near Trieste — " + label

	return rfs.ExtractedItem{
		GUID:        "osmer-rain-trieste:" + forecastDate.Format("2006-01-02") + ":" + issuedAt.Format("20060102T1504Z"),
		Title:       title,
		Link:        PageURL,
		Description: description,
		PubDate:     &issuedAt,
	}, true
}

var italianMonths = map[string]time.Month{
	"gennaio":   time.January,
	"febbraio":  time.February,
	"marzo":     time.March,
	"aprile":    time.April,
	"maggio":    time.May,
	"giugno":    time.June,
	"luglio":    time.July,
	"agosto":    time.August,
	"settembre": time.September,
	"ottobre":   time.October,
	"novembre":  time.November,
	"dicembre":  time.December,
}

var forecastDateRe = regexp.MustCompile(`\b(\d{1,2})\s+([A-Za-zÀ-ÿ]+)`)

func forecastDate(label string, issuedAt time.Time) (time.Time, error) {
	match := forecastDateRe.FindStringSubmatch(label)
	if match == nil {
		return time.Time{}, errors.New("osmer rain: forecast date not found")
	}
	month, ok := italianMonths[strings.ToLower(match[2])]
	if !ok {
		return time.Time{}, fmt.Errorf("osmer rain: unknown Italian month %q", match[2])
	}

	var day int
	if _, err := fmt.Sscanf(match[1], "%d", &day); err != nil {
		return time.Time{}, fmt.Errorf("osmer rain: unknown forecast day %q", match[1])
	}
	year := issuedAt.Year()
	date := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	// Around New Year the bulletin's next few days can cross into January.
	if month == time.January && issuedAt.Month() == time.December {
		date = date.AddDate(1, 0, 0)
	}
	return date, nil
}

var probabilityRowRe = regexp.MustCompile(`(?i)^probabilità di (.+?) \(%\) (\d{1,3})$`)

// probabilitySummary turns the bulletin precipitation probability rows into
// plain text so feed readers see the quantitative forecast with the prose.
func probabilitySummary(section *html.Node) string {
	var parts []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "tr" {
			if match := probabilityRowRe.FindStringSubmatch(tableRowText(n)); match != nil {
				parts = append(parts, fmt.Sprintf("Probabilità di %s: %s%%", match[1], match[2]))
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(section)
	return strings.Join(parts, " ")
}

func tableRowText(row *html.Node) string {
	var cells []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && (n.Data == "td" || n.Data == "th") {
			if text := normalizeSpace(textContent(n)); text != "" {
				cells = append(cells, text)
			}
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(row)
	return strings.Join(cells, " ")
}

func parseIssueTime(text string) (time.Time, error) {
	match := issueRe.FindStringSubmatch(normalizeSpace(text))
	if match == nil {
		return time.Time{}, errors.New("osmer rain: issue time not found")
	}

	zoneOffset := 1 * time.Hour
	if match[6] == "CEST" {
		zoneOffset = 2 * time.Hour
	}
	location := time.FixedZone(match[6], int(zoneOffset/time.Second))
	issue, err := time.ParseInLocation("02-01-2006 15:04", match[1]+"-"+match[2]+"-"+match[3]+" "+match[4]+":"+match[5], location)
	if err != nil {
		return time.Time{}, fmt.Errorf("osmer rain: parse issue time: %w", err)
	}
	return issue.UTC(), nil
}

func findByClass(root *html.Node, classes ...string) *html.Node {
	var found *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found != nil {
			return
		}
		if n.Type == html.ElementNode {
			all := true
			for _, class := range classes {
				if !hasClass(n, class) {
					all = false
					break
				}
			}
			if all {
				found = n
				return
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return found
}

func findByAttr(root *html.Node, elementName string, attrName string, attrValue string) *html.Node {
	var found *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found != nil {
			return
		}
		if n.Type == html.ElementNode && n.Data == elementName && (attrName == "" || attribute(n, attrName) == attrValue) {
			found = n
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return found
}

func findEmissione(root *html.Node) *html.Node {
	var found *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found != nil {
			return
		}
		if n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "small") && issueRe.MatchString(textContent(n)) {
			found = n
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return found
}

func hasClass(n *html.Node, class string) bool {
	for _, value := range strings.Fields(attribute(n, "class")) {
		if value == class {
			return true
		}
	}
	return false
}

func attribute(n *html.Node, name string) string {
	for _, attr := range n.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}

func textContent(n *html.Node) string {
	if n == nil {
		return ""
	}
	var parts []string
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			parts = append(parts, current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return htmlstdlib.UnescapeString(strings.Join(parts, ""))
}

func normalizeSpace(text string) string {
	return strings.TrimSpace(strings.Join(strings.Fields(text), " "))
}
