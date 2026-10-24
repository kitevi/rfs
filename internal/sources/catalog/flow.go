package catalog

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/html"

	"github.com/kitevi/rfs/internal/rfs"
)

// Flow follows one recurring general on a 4chan board through the board's
// catalog: each opening post whose subject names the general is one item.
type Flow struct {
	// Board is the board's short name, such as "g".
	Board string
	// General is the general's name without slashes, such as "ptg". It
	// matches subjects as "/ptg/" and prefixes item GUIDs.
	General string
	// Name is the general's usual long name. A subject or opening line that
	// says only this is the general's banner, not the thread's edition.
	Name string
	// ExtractVersion is this general's derivation version. Bump it when
	// Extract's output can change for a fixed catalog page. Each general
	// keeps its own, because a bump rebuilds that feed's stored history.
	ExtractVersion int
}

var (
	// PTG is /g/'s Private Trackers General.
	PTG = Flow{Board: "g", General: "ptg", Name: "Private Trackers General", ExtractVersion: 7}
	// Film is /tv/'s arthouse and classic cinema general. Matching is on the
	// subject alone: /film/ threads are the arthouse general by definition.
	Film = Flow{Board: "tv", General: "film", Name: "Arthouse & Classics", ExtractVersion: 6}
)

// Version reports the general's extraction version.
func (f Flow) Version() int { return f.ExtractVersion }

// PageURL is the board catalog API resource rfs polls.
func (f Flow) PageURL() string { return "https://a.4cdn.org/" + f.Board + "/catalog.json" }

// HumanURL is the human-facing catalog view linked from feed metadata.
func (f Flow) HumanURL() string {
	return "https://boards.4chan.org/" + f.Board + "/catalog#s=" + f.General
}

func (f Flow) marker() string { return "/" + f.General + "/" }

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

// Extract turns each opening post of the general in the board catalog into
// one RSS item. The catalog lists only live threads, so every match is emitted
// directly: unlike the former archive search, there is no superseded-only
// rule and no live thread to drop.
func (f Flow) Extract(page rfs.Page) ([]rfs.ExtractedItem, error) {
	var doc []catalogPage
	if err := json.Unmarshal([]byte(page), &doc); err != nil {
		return nil, fmt.Errorf("%s: parse catalog: %w", f.General, err)
	}

	var items []rfs.ExtractedItem
	matched := false
	for _, p := range doc {
		for _, t := range p.Threads {
			if !f.matchSubject(t.Sub) {
				continue
			}
			matched = true
			item, ok := f.extractThread(t)
			if !ok {
				continue
			}
			items = append(items, item)
		}
	}

	if !matched {
		return nil, fmt.Errorf("%s: no matching threads", f.General)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("%s: no valid threads", f.General)
	}
	return items, nil
}

// matchSubject reports whether a catalog subject names the general. The
// strict "/ptg/" form avoids false positives on subjects that merely contain
// the letters ptg.
func (f Flow) matchSubject(sub string) bool {
	return strings.Contains(strings.ToLower(sub), f.marker())
}

// stripThreadName removes the general's marker from a catalog subject so feed
// titles do not carry it. Dashes and spaces around the marker go too.
func (f Flow) stripThreadName(sub string) string {
	marker := f.marker()
	idx := strings.Index(strings.ToLower(sub), marker)
	if idx < 0 {
		return strings.TrimSpace(sub)
	}
	rest := sub[:idx] + sub[idx+len(marker):]
	return strings.TrimSpace(strings.TrimLeft(rest, " -\u2013\u2014\t"))
}

// title names a thread by its edition. A subject that says more than the
// general's name is the title. Otherwise the edition is the opening post's
// first line that mentions an edition, or else its first line, among the lines
// before the first greentext. Lines that only repeat the general's name are the
// banner, and greentext starts the general's standing boilerplate.
func (f Flow) title(sub, description string) string {
	if subject := f.stripThreadName(sub); subject != "" && fold(subject) != fold(f.Name) {
		return subject
	}
	var first, edition string
	for _, line := range strings.Split(description, "\n") {
		line = f.stripThreadName(line)
		if line == "" || fold(line) == fold(f.Name) {
			continue
		}
		if strings.HasPrefix(line, ">") {
			break
		}
		if first == "" {
			first = line
		}
		if strings.Contains(strings.ToLower(line), "dition") {
			edition = line
			break
		}
	}
	if edition == "" {
		edition = first
	}
	if edition == "" {
		return f.Name
	}
	return f.Name + " \u2014 " + edition
}

// fold keeps only lowercase letters and digits, so "Arthouse & Classics" and
// "arthouse and classics" differ but spacing and case do not.
func fold(text string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// RebuildStored uses the same derivation as fresh catalog extraction.
func (f Flow) RebuildStored(old rfs.Item) (rfs.Item, error) {
	return rfs.RebuildFromSavedInput(old, f.General, f.marker(), f.extractThread, f.stripThreadName)
}

func (f Flow) extractThread(t catalogThread) (rfs.ExtractedItem, bool) {
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
	title := f.title(t.Sub, description)
	pubDate := time.Unix(t.Time, 0).UTC()
	replies := t.Replies
	if replies < 0 {
		replies = 0
	}
	raw, _ := json.Marshal(t) // catalogThread contains only JSON-safe fields.
	return rfs.ExtractedItem{
		Metadata:    string(raw),
		GUID:        f.General + ":" + id,
		Title:       title,
		Link:        "https://boards.4chan.org/" + f.Board + "/thread/" + id + "/",
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
