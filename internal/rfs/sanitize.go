package rfs

import (
	stdhtml "html"
	"net/url"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// allowedTags are the elements a Flow may use in an HTML item description.
var allowedTags = map[string]bool{
	"br": true, "code": true, "em": true, "h3": true, "h4": true,
	"img": true, "li": true, "ol": true, "p": true, "pre": true,
	"span": true, "strong": true, "ul": true,
}

// droppedTags are dropped with their whole subtree: a browser would execute,
// load, or navigate through them, and no Flow description needs them.
var droppedTags = map[string]bool{
	"base": true, "embed": true, "form": true, "head": true, "iframe": true,
	"link": true, "math": true, "meta": true, "noscript": true, "object": true,
	"script": true, "style": true, "svg": true, "template": true, "title": true,
}

// safeHTML renders a Flow-built description for the browser view. Flows are
// expected to escape upstream text themselves; this allowlist is the second
// line of defence, so a Flow mistake cannot turn the feed into a script or
// request sink. Unknown elements are unwrapped to their text, and an element
// that a browser would execute or fetch is dropped with its subtree.
func safeHTML(fragment string) string {
	context := &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div}
	nodes, err := html.ParseFragment(strings.NewReader(fragment), context)
	if err != nil {
		return stdhtml.EscapeString(fragment)
	}
	var b strings.Builder
	for _, node := range nodes {
		writeSafeHTML(&b, node)
	}
	return b.String()
}

func writeSafeHTML(b *strings.Builder, node *html.Node) {
	if node.Type == html.TextNode {
		b.WriteString(stdhtml.EscapeString(node.Data))
		return
	}
	if node.Type != html.ElementNode {
		return
	}

	tag := strings.ToLower(node.Data)
	if droppedTags[tag] {
		return
	}
	if !allowedTags[tag] {
		writeSafeChildren(b, node)
		return
	}
	attrs := safeAttributes(tag, node)
	if tag == "img" && !hasAttribute(attrs, "src") {
		return // an image without a safe source is not worth keeping
	}

	b.WriteString("<" + tag)
	for _, attribute := range attrs {
		b.WriteString(" " + attribute.name + "=\"" + stdhtml.EscapeString(attribute.value) + "\"")
	}
	if tag == "br" || tag == "img" {
		b.WriteString(">")
		return
	}
	b.WriteString(">")
	writeSafeChildren(b, node)
	b.WriteString("</" + tag + ">")
}

func writeSafeChildren(b *strings.Builder, node *html.Node) {
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		writeSafeHTML(b, child)
	}
}

type safeAttribute struct{ name, value string }

func safeAttributes(tag string, node *html.Node) []safeAttribute {
	attrs := make([]safeAttribute, 0, len(node.Attr))
	for _, attribute := range node.Attr {
		name := strings.ToLower(attribute.Key)
		if !attributeAllowed(tag, name, attribute.Val) {
			continue
		}
		attrs = append(attrs, safeAttribute{name: name, value: attribute.Val})
	}
	return attrs
}

func attributeAllowed(tag, name, value string) bool {
	switch name {
	case "src":
		return tag == "img" && safeHTTPURL(value)
	case "alt", "width", "height", "title":
		return tag == "img"
	case "style":
		return tag != "img" && safeStyleValue(value)
	}
	return false
}

func hasAttribute(attrs []safeAttribute, name string) bool {
	for _, attribute := range attrs {
		if attribute.name == name {
			return true
		}
	}
	return false
}

// safeHTTPURL accepts absolute HTTP(S) URLs, the only kind rfs publishes.
func safeHTTPURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

// safeStyleValue accepts the small vocabulary Flows use: colours and
// white-space declarations. It rejects anything that could load a resource or
// escape the declaration list.
func safeStyleValue(value string) bool {
	lower := strings.ToLower(value)
	for _, banned := range []string{"url(", "expression", "javascript:", "@import", "\\", "<", ">"} {
		if strings.Contains(lower, banned) {
			return false
		}
	}
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune(" #-;:,.%()", r):
		default:
			return false
		}
	}
	return true
}
