package catalog

import (
	"encoding/json"
	"fmt"
	"slices"
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
	// StandingStart and StandingEnd are the first and last lines of the block
	// of links, rules and reminders that every opening post of the general
	// repeats. Item descriptions leave that block out. Empty when openers
	// carry no such block, and an opening post that does not carry the block
	// whole keeps all of its text.
	StandingStart string
	StandingEnd   string
	// ExtractVersion is this general's derivation version. Bump it when
	// Extract's output can change for a fixed catalog page. Each general
	// keeps its own, because a bump rebuilds that feed's stored history.
	ExtractVersion int
}

var (
	// PTG is /g/'s Private Trackers General.
	PTG = Flow{
		Board: "g", General: "ptg", Name: "Private Trackers General", ExtractVersion: 8,
		StandingStart: ">Not sure what private trackers are all about?",
		StandingEnd:   ">If starting out you should interview for RED, rank to Torrent Master, wait on account age, and get invites from there. THAT'S IT THAT IS ALL YOU HAVE TO DO",
	}
	// Film is /tv/'s arthouse and classic cinema general. Matching is on the
	// subject alone: /film/ threads are the arthouse general by definition.
	Film = Flow{Board: "tv", General: "film", Name: "Arthouse & Classics", ExtractVersion: 7}
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

// title names a thread by its edition and returns the edition's text, so the
// opening post's copy of it can be left out. A subject that says more than the
// general's name is the title. Otherwise the edition is the first line that
// mentions one, or else the first line, among the lines before the first
// greentext, which starts the poster's own talk.
func (f Flow) title(sub string, lines []string) (string, string) {
	if subject := f.stripThreadName(sub); subject != "" && fold(subject) != fold(f.Name) {
		return subject, subject
	}
	banner := true
	first, edition := -1, -1
	for i, raw := range lines {
		line := f.stripThreadName(raw)
		if line == "" || (banner && fold(line) == fold(f.Name)) {
			continue
		}
		banner = false
		if strings.HasPrefix(line, ">") {
			break
		}
		if first < 0 {
			first = i
		}
		if strings.Contains(strings.ToLower(line), "dition") {
			edition = i
			break
		}
	}
	named := edition
	if named < 0 {
		named = first
	}
	if named < 0 {
		return f.Name, ""
	}
	text := f.stripThreadName(lines[named])
	return f.Name + " \u2014 " + text, text
}

// body is what the thread itself says: the opening post without the banner and
// the edition line the title already carries. Its caller has already removed
// the parts that describe the general rather than the thread.
func (f Flow) body(lines []string, edition string) string {
	repeat := -1
	if edition != "" {
		repeat = slices.IndexFunc(lines, func(line string) bool { return fold(f.stripThreadName(line)) == fold(edition) })
	}
	kept := make([]string, 0, len(lines))
	banner := true
	for i, line := range lines {
		if banner && (line == "" || fold(f.stripThreadName(line)) == fold(f.Name)) {
			continue
		}
		banner = false
		if i == repeat {
			continue
		}
		kept = append(kept, line)
	}
	return normalizeText(strings.Join(kept, "\n"))
}

// ownLines removes what describes the general rather than this thread.
func (f Flow) ownLines(description string) []string {
	lines := strings.Split(description, "\n")
	if first, last := f.standingBounds(lines); first >= 0 {
		lines = slices.Concat(lines[:first:first], lines[last+1:])
	}
	return slices.DeleteFunc(lines, func(line string) bool { return previousThread(line) || postReference(line) })
}

// standingBounds returns the indexes of the standing block's first and last
// lines, or -1 for both when the opening post does not carry the whole block.
// A rewritten block is left in place rather than cut at a guess: showing the
// general's rules again costs a reader less than swallowing what a poster wrote.
func (f Flow) standingBounds(lines []string) (int, int) {
	if f.StandingStart == "" || f.StandingEnd == "" {
		return -1, -1
	}
	first := slices.IndexFunc(lines, func(line string) bool { return fold(line) == fold(f.StandingStart) })
	if first < 0 {
		return -1, -1
	}
	last := slices.IndexFunc(lines[first:], func(line string) bool { return fold(line) == fold(f.StandingEnd) })
	if last < 0 {
		return -1, -1
	}
	return first, first + last
}

// previousThread reports whether a line is the pointer to the general's
// previous thread, a label and a post number ("Previous thread: >>110362431",
// "Previous: >>110305003").
func previousThread(line string) bool {
	label, rest, ok := strings.Cut(strings.ToLower(line), ":")
	return ok && (label == "previous" || label == "previous thread") && strings.Contains(rest, ">>")
}

// postReference is a line that only points at another post, ">>110369071". It
// says nothing on its own, and the numbers it points at live inside 4chan, so
// the opening post's lone references are dropped. References that carry the
// general's news keep their text and their pointer together.
func postReference(line string) bool {
	rest, ok := strings.CutPrefix(line, ">>")
	return ok && rest != "" && strings.Trim(rest, "0123456789 ") == ""
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
	lines := f.ownLines(description)
	title, edition := f.title(t.Sub, lines)
	description = f.body(lines, edition)
	pubDate := time.Unix(t.Time, 0).UTC()
	replies := max(t.Replies, 0)
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
	for line := range strings.SplitSeq(raw, "\n") {
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
