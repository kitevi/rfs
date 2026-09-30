package malstack

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

const comparisonVersion = 1

// comparison is the opaque, versioned baseline payload stored in an item's
// description. It is deliberately separate from rendered feed text.
type comparison struct {
	Version int      `json:"version"`
	Notes   []string `json:"notes"`
}

type animeEntry struct {
	id    string
	title string
	notes []string
}

type stack struct {
	anime []animeEntry
}

var entryCountPattern = regexp.MustCompile(`([0-9]+)\s+Entr(?:y|ies)`)

// parseStack reads the canonical stack detail container. Any shape it does
// not recognize fails closed so an unparsed page can never look like removals.
func parseStack(doc *html.Node) (stack, error) {
	if !isStackPage(doc) {
		return stack{}, fmt.Errorf("unrecognized stack page: not %s", PageURL)
	}
	declared, err := declaredCount(doc)
	if err != nil {
		return stack{}, err
	}
	detail := findNode(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" &&
			hasClass(n, "content-left") && hasClass(n, "stacks-detail")
	})
	if detail == nil {
		return stack{}, fmt.Errorf("unrecognized stack page: missing detail container")
	}
	if pagination := findNode(detail, func(n *html.Node) bool {
		return n.Type == html.ElementNode && (hasClass(n, "pagination") || attr(n, "rel") == "next")
	}); pagination != nil {
		return stack{}, fmt.Errorf("unrecognized stack page: paginated collection")
	}
	list := findNode(detail, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "list-anime-list")
	})
	if list == nil {
		return stack{}, fmt.Errorf("unrecognized stack page: missing anime list")
	}
	var result stack
	seen := map[string]bool{}
	for card := list.FirstChild; card != nil; card = card.NextSibling {
		if card.Type != html.ElementNode || card.Data != "div" || !hasClass(card, "seasonal-anime") {
			continue
		}
		entry, err := parseAnime(card)
		if err != nil {
			return stack{}, err
		}
		if seen[entry.id] {
			return stack{}, fmt.Errorf("invalid stack entry: duplicate anime %s", entry.id)
		}
		seen[entry.id] = true
		result.anime = append(result.anime, entry)
	}
	if len(result.anime) != declared {
		return stack{}, fmt.Errorf("incomplete stack page: declared %d entries, parsed %d", declared, len(result.anime))
	}
	return result, nil
}

// isStackPage verifies the page's own canonical URL metadata names the watched
// stack. A login or challenge page has no og:url and is rejected here.
func isStackPage(doc *html.Node) bool {
	meta := findNode(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "meta" && attr(n, "property") == "og:url"
	})
	if meta == nil {
		return false
	}
	parsed, err := url.Parse(strings.TrimSpace(attr(meta, "content")))
	if err != nil {
		return false
	}
	return parsed.Host == "myanimelist.net" && parsed.Path == "/stacks/"+StackID
}

// declaredCount reads the entry total MAL appends to og:description. The
// trailing mention wins: the curator's own description may itself say
// "N Entries" earlier. A wrong count here only rejects a valid page (fail
// closed); it can never publish changes.
func declaredCount(doc *html.Node) (int, error) {
	meta := findNode(doc, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "meta" && attr(n, "property") == "og:description"
	})
	if meta == nil {
		return 0, fmt.Errorf("unrecognized stack page: missing description metadata")
	}
	matches := entryCountPattern.FindAllStringSubmatch(attr(meta, "content"), -1)
	if len(matches) == 0 {
		return 0, fmt.Errorf("unrecognized stack page: missing entry count")
	}
	count := 0
	for _, digit := range matches[len(matches)-1][1] {
		count = count*10 + int(digit-'0')
	}
	return count, nil
}

func parseAnime(card *html.Node) (animeEntry, error) {
	link := findNode(card, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "a" && hasClass(n, "link-title")
	})
	if link == nil {
		return animeEntry{}, fmt.Errorf("invalid stack entry: missing title link")
	}
	id, ok := animeID(attr(link, "href"))
	if !ok {
		return animeEntry{}, fmt.Errorf("invalid stack entry: unrecognized anime link")
	}
	title := strings.TrimSpace(nodeText(link))
	if title == "" {
		return animeEntry{}, fmt.Errorf("invalid stack entry: empty title for anime %s", id)
	}
	notes, err := noteLines(card)
	if err != nil {
		return animeEntry{}, err
	}
	return animeEntry{id: id, title: title, notes: notes}, nil
}

func hasClass(n *html.Node, class string) bool {
	for _, field := range strings.Fields(attr(n, "class")) {
		if field == class {
			return true
		}
	}
	return false
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func findNode(n *html.Node, match func(*html.Node) bool) *html.Node {
	if match(n) {
		return n
	}
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		if found := findNode(child, match); found != nil {
			return found
		}
	}
	return nil
}

func nodeText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

// noteLines reads the curator's notes from the card's explicit wrapper. A
// card without one is rejected rather than treated as an emptied note: an
// unrecognized layout must not look like a removed note. Only an explicit,
// empty wrapper canonicalizes to no lines.
func noteLines(card *html.Node) ([]string, error) {
	intro := findNode(card, func(n *html.Node) bool {
		return n.Type == html.ElementNode && n.Data == "div" && hasClass(n, "intro")
	})
	if intro == nil {
		return nil, fmt.Errorf("invalid stack entry: missing curator notes")
	}
	return canonicalLines(intro), nil
}

var blockElements = map[string]bool{
	"address": true, "article": true, "blockquote": true, "div": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"li": true, "ol": true, "p": true, "section": true, "table": true,
	"td": true, "th": true, "tr": true, "ul": true,
}

// canonicalLines converts a notes subtree to plain lines: entities already
// decoded by the parser, meaningful line order and repetitions preserved, and
// links kept as inert text. Formatting-only markup is discarded.
func canonicalLines(root *html.Node) []string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			b.WriteString(n.Data)
			return
		case html.ElementNode:
			switch n.Data {
			case "script", "style", "template", "head":
				return
			case "br":
				b.WriteByte('\n')
				return
			}
			if blockElements[n.Data] {
				b.WriteByte('\n')
			}
		}
		if n.Type == html.ElementNode && n.Data == "a" && b.Len() > 0 {
			// Adjacent inline elements would otherwise glue words together.
			if last := b.String()[b.Len()-1]; last != ' ' && last != '\n' && last != '(' {
				b.WriteByte(' ')
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if n.Type == html.ElementNode {
			if href := safeHref(attr(n, "href")); href != "" {
				b.WriteString(" (" + href + ")")
			}
			if blockElements[n.Data] {
				b.WriteByte('\n')
			}
		}
	}
	walk(root)
	return normalizeLines(b.String())
}

func safeHref(href string) string {
	href = strings.TrimSpace(href)
	for _, scheme := range []string{"https://", "http://"} {
		if strings.HasPrefix(href, scheme) {
			return href
		}
	}
	return ""
}

// normalizeLines trims horizontal whitespace per line, collapses repeated
// blank lines, drops outer blanks, and never returns nil so an explicitly
// empty note is distinguishable from a missing wrapper.
func normalizeLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.ReplaceAll(text, "\u00a0", " ")
	lines := []string{}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.Join(strings.Fields(raw), " ")
		if line == "" {
			if len(lines) > 0 && lines[len(lines)-1] != "" {
				lines = append(lines, "")
			}
			continue
		}
		lines = append(lines, line)
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
