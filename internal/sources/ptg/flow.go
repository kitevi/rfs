package ptg

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/ppowo/rfs/internal/rfs"
)

const (
	// PageURL is the board catalog API resource rfs polls for /ptg/ threads.
	PageURL = "https://a.4cdn.org/g/catalog.json"
	// HumanURL is the human-facing catalog view linked from feed metadata.
	HumanURL = "https://boards.4chan.org/g/catalog#s=ptg"

	threadBaseURL = "https://boards.4chan.org/g/thread/"
)

// ExtractVersion is the derivation version for the /ptg/ Flow. Bump it when
// Extract's output can change for a fixed catalog page.
const ExtractVersion = 5

type Flow struct{}

// Version reports the /ptg/ extraction version.
func (Flow) Version() int { return ExtractVersion }

type catalogPage struct {
	Threads []catalogThread `json:"threads"`
}

type catalogThread struct {
	No      int64  `json:"no"`
	Sub     string `json:"sub"`
	Com     string `json:"com"`
	Time    int64  `json:"time"`
	Resto   int64  `json:"resto"`
	Replies int    `json:"replies"`
	Images  int    `json:"images"`
}

// Extract turns each /ptg/ opening post in the board catalog into one RSS
// item. The catalog lists only live threads, so every match is emitted
// directly: unlike the former archive search, there is no superseded-only
// rule and no live thread to drop.
func (Flow) Extract(page rfs.Page) ([]rfs.ExtractedItem, error) {
	var doc []catalogPage
	if err := json.Unmarshal([]byte(page), &doc); err != nil {
		return nil, fmt.Errorf("ptg: parse catalog: %w", err)
	}

	var items []rfs.ExtractedItem
	matched := false
	for _, p := range doc {
		for _, t := range p.Threads {
			if !matchSubject(t.Sub) {
				continue
			}
			matched = true
			item, ok := extractThread(t)
			if !ok {
				continue
			}
			items = append(items, item)
		}
	}

	if !matched {
		return nil, errors.New("ptg: no matching threads")
	}
	if len(items) == 0 {
		return nil, errors.New("ptg: no valid threads")
	}
	return items, nil
}

// matchSubject reports whether a catalog subject names the /ptg/ general.
// The strict "/ptg/" form avoids false positives on subjects that merely
// contain the letters ptg.
func matchSubject(sub string) bool {
	return strings.Contains(strings.ToLower(sub), "/ptg/")
}

// stripThreadName removes the /ptg/ marker from a catalog subject so feed
// titles do not carry it. Dashes and spaces around the marker go too.
func stripThreadName(sub string) string {
	idx := strings.Index(strings.ToLower(sub), "/ptg/")
	if idx < 0 {
		return strings.TrimSpace(sub)
	}
	rest := sub[:idx] + sub[idx+len("/ptg/"):]
	return strings.TrimSpace(strings.TrimLeft(rest, " -\u2013\u2014\t"))
}

func extractThread(t catalogThread) (rfs.ExtractedItem, bool) {
	if t.No <= 0 || t.Resto != 0 {
		return rfs.ExtractedItem{}, false
	}
	if t.Sub == "" || t.Com == "" || t.Time <= 0 {
		return rfs.ExtractedItem{}, false
	}
	id := strconv.FormatInt(t.No, 10)
	description := decodeComFragment(t.Com)
	if description == "" {
		return rfs.ExtractedItem{}, false
	}
	title := stripThreadName(t.Sub)
	if firstLine := strings.SplitN(description, "\n", 2)[0]; firstLine != "" {
		if title == "" {
			title = firstLine
		} else {
			title += " \u2014 " + firstLine
		}
	}
	pubDate := time.Unix(t.Time, 0).UTC()
	replies := t.Replies
	if replies < 0 {
		replies = 0
	}
	return rfs.ExtractedItem{
		GUID:        "ptg:" + id,
		Title:       title,
		Link:        threadBaseURL + id + "/",
		Description: description,
		PubDate:     &pubDate,
		Replies:     replies,
	}, true
}

// decodeComFragment turns an opening-post HTML fragment (br, wbr, entities,
// quotelink anchors) into normalized plain text.
func decodeComFragment(fragment string) string {
	doc, err := html.Parse(strings.NewReader("<div>" + fragment + "</div>"))
	if err != nil {
		return ""
	}
	return textContent(doc)
}

func textContent(root *html.Node) string {
	var raw strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			raw.WriteString(n.Data)
			return
		}
		if n.Type == html.ElementNode && n.Data == "br" {
			raw.WriteByte('\n')
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return normalizeText(raw.String())
}

func normalizeText(raw string) string {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	raw = strings.ReplaceAll(raw, "\u00a0", " ")

	var lines []string
	blank := false
	for _, line := range strings.Split(raw, "\n") {
		line = strings.Join(strings.Fields(line), " ")
		if line == "" {
			if len(lines) > 0 && !blank {
				lines = append(lines, "")
				blank = true
			}
			continue
		}
		lines = append(lines, line)
		blank = false
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
