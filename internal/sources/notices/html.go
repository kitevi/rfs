package notices

// HTML helpers shared by the operator parsers. Every parser reads bytes rfs
// already fetched, so these only walk a parsed tree.

import (
	"strings"

	"golang.org/x/net/html"
)

// Attr returns an attribute value, or an empty string when it is absent.
func Attr(n *html.Node, key string) string {
	if n == nil {
		return ""
	}
	for _, attribute := range n.Attr {
		if attribute.Key == key {
			return attribute.Val
		}
	}
	return ""
}

// HasClass reports whether an element carries a class.
func HasClass(n *html.Node, class string) bool {
	for _, candidate := range strings.Fields(Attr(n, "class")) {
		if candidate == class {
			return true
		}
	}
	return false
}

// FindFirst returns the first descendant element with the tag, or the node
// itself when it matches.
func FindFirst(root *html.Node, tag string) *html.Node {
	if root == nil {
		return nil
	}
	if root.Type == html.ElementNode && root.Data == tag {
		return root
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if found := FindFirst(child, tag); found != nil {
			return found
		}
	}
	return nil
}

// FindByClass returns the first descendant element with the tag and class.
func FindByClass(root *html.Node, tag, class string) *html.Node {
	if root == nil {
		return nil
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == tag && HasClass(child, class) {
			return child
		}
		if found := FindByClass(child, tag, class); found != nil {
			return found
		}
	}
	return nil
}

// TextOf renders the text of a subtree, keeping block boundaries as newlines
// and skipping the elements that carry no readable text.
func TextOf(root *html.Node) string {
	if root == nil {
		return ""
	}
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			builder.WriteString(n.Data)
			return
		}
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "noscript":
				return
			case "br":
				builder.WriteString("\n")
				return
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
		if n.Type == html.ElementNode && isBlockTag(n.Data) {
			builder.WriteString("\n")
		}
	}
	walk(root)
	return NormalizeText(builder.String())
}

func isBlockTag(tag string) bool {
	switch tag {
	case "address", "article", "blockquote", "div", "dd", "dt", "fieldset", "figcaption", "figure", "footer", "form", "h1", "h2", "h3", "h4", "h5", "h6", "header", "hr", "li", "main", "nav", "ol", "p", "pre", "section", "table", "tbody", "td", "tfoot", "th", "thead", "tr", "ul":
		return true
	}
	return false
}

// NormalizeText collapses whitespace inside a line and drops empty lines, so a
// formatting-only upstream change does not read as a text change.
func NormalizeText(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		if line != "" {
			parts = append(parts, line)
		}
	}
	return tightenPunctuation(strings.Join(parts, "\n"))
}

// tightenPunctuation removes the spaces that re-serialized markup introduces
// between inline elements and punctuation.
func tightenPunctuation(text string) string {
	for _, punct := range []string{".", ",", ";", ":", "!", "?", ")", "»"} {
		text = strings.ReplaceAll(text, " "+punct, punct)
	}
	return strings.ReplaceAll(text, "( ", "(")
}
