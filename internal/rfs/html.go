package rfs

import (
	"bytes"
	"html/template"
	"time"
)

const htmlStyle = `
body { font: 16px/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", system-ui, sans-serif; max-width: 720px; margin: 2rem auto; padding: 0 1rem; color: #1a1a1a; }
header { margin-bottom: 2rem; }
h1 { font-size: 1.5rem; margin: 0 0 0.25rem; }
a { color: #2a6df4; }
.meta { color: #666; font-size: 0.9rem; margin: 0.25rem 0; }
ul { list-style: none; padding: 0; }
li { padding: 0.75rem 0; border-bottom: 1px solid #eee; }
li:last-child { border-bottom: none; }
.item-title { font-weight: 600; }
.item-date { color: #666; font-size: 0.85rem; }
.rss { font-size: 0.8rem; }
footer.version { color: #999; font-size: 0.8rem; margin-top: 2rem; }
`

const indexTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>rfs</title>
<style>` + htmlStyle + `</style>
</head>
<body>
<header>
<h1>rfs</h1>
<p class="meta">Feeds served by rfs.</p>
</header>
<ul>
{{ range .Sources }}<li>
<a class="item-title" href="/feeds/{{ .ID }}.html">{{ .Meta.Title }}</a>
{{ if .Meta.Description }}<p class="meta">{{ .Meta.Description }}</p>{{ end }}
<a class="rss" href="/feeds/{{ .ID }}.xml">RSS feed</a>
</li>
{{ end }}</ul>
<footer class="version">{{ .Build }}</footer>
</body>
</html>`

const feedTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{ .Meta.Title }}</title>
<style>` + htmlStyle + `</style>
</head>
<body>
<header>
<h1>{{ .Meta.Title }}</h1>
{{ if .Meta.Description }}<p class="meta">{{ .Meta.Description }}</p>{{ end }}
<p class="meta"><a href="{{ .Meta.Link }}">source</a> · <a class="rss" href="/feeds/{{ .ID }}.xml">RSS feed</a></p>
</header>
<ul>
{{ range .Items }}<li>
<a class="item-title" href="{{ .Link }}">{{ .Title }}</a>
<p class="item-date">{{ .PubDate }}</p>
{{ if .Description }}<div class="meta">{{ .Description }}</div>{{ end }}
</li>
{{ else }}<li class="meta">No items yet.</li>
{{ end }}</ul>
<footer class="version">{{ .Build }}</footer>
</body>
</html>`

type feedItemView struct {
	Title       string
	Link        string
	Description any
	PubDate     string
}

type feedView struct {
	ID    string
	Meta  SourceMeta
	Items []feedItemView
	Build BuildInfo
}

type indexView struct {
	Sources []Source
	Build   BuildInfo
}

// RenderHTMLIndex renders an HTML listing of all sources, with build info in
// the footer.
func RenderHTMLIndex(sources []Source, build BuildInfo) ([]byte, error) {
	return renderTemplate("index", indexTemplate, indexView{Sources: sources, Build: build})
}

// RenderHTMLFeed renders a single source's items as an HTML page, with build
// info in the footer.
func RenderHTMLFeed(sourceID string, meta SourceMeta, items []Item, build BuildInfo) ([]byte, error) {
	view := feedView{
		ID:    sourceID,
		Meta:  meta,
		Items: make([]feedItemView, 0, len(items)),
		Build: build,
	}
	for _, item := range items {
		var description any = item.Description
		if meta.ItemDescriptionsHTML {
			description = template.HTML(item.Description)
		}
		view.Items = append(view.Items, feedItemView{
			Title:       item.Title,
			Link:        item.Link,
			Description: description,
			PubDate:     formatHTMLDate(item.PubDate),
		})
	}
	return renderTemplate("feed", feedTemplate, view)
}

// renderTemplate parses text as a template named name and executes it with
// data, returning the rendered bytes. It deduplicates the render shape shared
// by RenderHTMLIndex and RenderHTMLFeed.
func renderTemplate(name, text string, data any) ([]byte, error) {
	tmpl, err := template.New(name).Parse(text)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func formatHTMLDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2 January 2006")
}
