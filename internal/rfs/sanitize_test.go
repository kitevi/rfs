package rfs

import (
	"strings"
	"testing"
)

func TestSafeHTMLDropsExecutableMarkup(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"script element", `<script>alert(1)</script>kept`, []string{"kept"}},
		{"style element", `<style>body{background:url(javascript:x)}</style>kept`, []string{"kept"}},
		{"iframe", `<iframe src="https://evil.test/x">fallback</iframe>kept`, []string{"kept"}},
		{"event handler", `<p onclick="steal()">hi</p>`, []string{"<p>hi</p>"}},
		{"img handler", `<img src="https://cdn.test/a.jpg" onerror="steal()">`, []string{`src="https://cdn.test/a.jpg"`}},
		{"javascript url", `<img src="javascript:alert(1)">`, []string{}},
		{"data url", `<img src="data:text/html,<script>alert(1)</script>">`, []string{}},
		{"unknown inline unwrapped", `<b>bold</b>`, []string{"bold"}},
		{"text is escaped", `1 < 2 & 3 > 2`, []string{"1 &lt; 2 &amp; 3 &gt; 2"}},
	}
	forbidden := []string{"<script", "<style", "<iframe", "onclick", "onerror", "javascript:", "<b>", "data:text/html"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := safeHTML(tc.in)
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Fatalf("safeHTML(%q) = %q, want it to contain %q", tc.in, got, want)
				}
			}
			for _, bad := range forbidden {
				if strings.Contains(strings.ToLower(got), bad) {
					t.Fatalf("safeHTML(%q) = %q, still contains %q", tc.in, got, bad)
				}
			}
		})
	}
}

func TestSafeHTMLKeepsTheMarkupFlowsBuild(t *testing.T) {
	seadexDiff := `<h3>Best</h3><pre style="white-space:pre-wrap"><span style="color:#067647">+ Group</span>
<span style="color:#b42318">- Old</span></pre>`
	got := safeHTML(seadexDiff)
	for _, want := range []string{
		"<h3>Best</h3>",
		`<pre style="white-space:pre-wrap">`,
		`<span style="color:#067647">+ Group</span>`,
		`<span style="color:#b42318">- Old</span>`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("safeHTML(%q) = %q, want it to contain %q", seadexDiff, got, want)
		}
	}

	cover := `<img src="https://cdn.test/cover.jpg" alt="Title" width="100">`
	got = safeHTML(cover)
	for _, want := range []string{`src="https://cdn.test/cover.jpg"`, `alt="Title"`, `width="100"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("safeHTML(%q) = %q, want it to contain %q", cover, got, want)
		}
	}

	notes := `<p>Added to the stack.</p><pre style="white-space:pre-wrap">line one
line two</pre>`
	got = safeHTML(notes)
	for _, want := range []string{"<p>Added to the stack.</p>", "line one", "line two"} {
		if !strings.Contains(got, want) {
			t.Fatalf("safeHTML(%q) = %q, want it to contain %q", notes, got, want)
		}
	}
}

func TestRenderHTMLFeedSanitizesFlowDescriptions(t *testing.T) {
	meta := SourceMeta{Title: "t", Link: "https://example.com", ItemDescriptionsHTML: true}
	items := []Item{{GUID: "g", Title: "t", Link: "https://example.com", Description: `<img src="x" onerror="steal()"><script>alert(1)</script>kept`}}
	body, err := RenderHTMLFeed("source", meta, items, BuildInfo{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	rendered := string(body)
	for _, bad := range []string{"<script", "onerror=", "alert(1)"} {
		if strings.Contains(rendered, bad) {
			t.Fatalf("rendered feed still contains %q: %s", bad, rendered)
		}
	}
	if !strings.Contains(rendered, "kept") {
		t.Fatalf("rendered feed dropped the safe text: %s", rendered)
	}
}
