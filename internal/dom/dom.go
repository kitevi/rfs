// Package dom holds the HTML tree helpers Flows share.
package dom

import (
	"slices"
	"strings"

	"golang.org/x/net/html"
)

// Attr returns the value of n's attribute, or "" when it has none.
func Attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

// HasClass reports whether n's class list names class.
func HasClass(n *html.Node, class string) bool {
	return slices.Contains(strings.Fields(Attr(n, "class")), class)
}

// Text returns the text of n and everything beneath it, as written.
func Text(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for node := range n.Descendants() {
		if node.Type == html.TextNode {
			b.WriteString(node.Data)
		}
	}
	return b.String()
}

// Collapse trims text and folds each run of white space into one space.
func Collapse(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// All returns root and the nodes beneath it that match, in document order.
func All(root *html.Node, match func(*html.Node) bool) []*html.Node {
	var found []*html.Node
	if match(root) {
		found = append(found, root)
	}
	for node := range root.Descendants() {
		if match(node) {
			found = append(found, node)
		}
	}
	return found
}

// Find returns the first of root and the nodes beneath it that matches, or nil.
func Find(root *html.Node, match func(*html.Node) bool) *html.Node {
	if match(root) {
		return root
	}
	for node := range root.Descendants() {
		if match(node) {
			return node
		}
	}
	return nil
}

// Elements returns every element with the given tag name, in document order.
func Elements(root *html.Node, tag string) []*html.Node {
	return All(root, func(n *html.Node) bool { return n.Type == html.ElementNode && n.Data == tag })
}

// WithClass returns every element whose class list names class.
func WithClass(root *html.Node, class string) []*html.Node {
	return All(root, func(n *html.Node) bool { return n.Type == html.ElementNode && HasClass(n, class) })
}
