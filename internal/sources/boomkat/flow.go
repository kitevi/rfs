// Package boomkat extracts Boomkat's Record Of The Week.
//
// The boomkat.com homepage is a stack of page blocks. One of them carries a
// header flag that reads Record Of The Week and names a single release: its
// formats, artist, title, a review excerpt and a Read More link. The weekly
// roundup's Records of the Week is a different list, of about ten releases,
// and is not read. The block is found by its flag, not by its layout name, so
// a change of template does not lose it.
//
// Each Record Of The Week is announced once, when rfs first sees it flagged,
// and stays in the feed after the homepage moves on. The first observation
// announces the current pick.
package boomkat

import (
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/net/html"

	"github.com/kitevi/rfs/internal/dom"
	"github.com/kitevi/rfs/internal/rfs"
)

const (
	// PageURL is the homepage, the only page that flags the Record Of The Week.
	PageURL = "https://boomkat.com/"

	// HumanURL is the page a reader opens to see the same pick.
	HumanURL = PageURL

	// siteHost is the only host whose links are read.
	siteHost = "boomkat.com"

	// ExtractVersion is the derivation version for this Flow. Bump it whenever
	// Extract's output can change for a fixed page.
	ExtractVersion = 1

	// CheckpointVersion is the announcement checkpoint schema version.
	CheckpointVersion = 1

	// guidPrefix makes a release's Boomkat ID recoverable from its GUID and
	// keeps identities unique across Sources.
	guidPrefix = "boomkat:record-of-the-week:"

	// flagLabel is the header flag text that marks the block.
	flagLabel = "Record Of The Week"

	// readMore is the button that ends the block. It links to the release too.
	readMore = "Read More"
)

// releasePath matches a release page, /artists/<artist>/releases/<id>/<title>.
// The ID is the release's own; the slugs around it follow its names.
var releasePath = regexp.MustCompile(`^/artists/([^/]+)/releases/([a-z0-9]+)/[^/]+$`)

// releaseID matches an ID on its own, as GUIDs and the checkpoint hold it.
var releaseID = regexp.MustCompile(`^[a-z0-9]+$`)

// Flow extracts the Record Of The Week and decides whether it is new.
type Flow struct{}

var _ rfs.AnnouncementFlow = Flow{}

// Version reports the derivation version; rfs re-fetches the full page when
// the stored version differs rather than trusting a conditional answer.
func (Flow) Version() int { return ExtractVersion }

// Extract returns the release of each block flagged Record Of The Week, in
// page order. The homepage flags one. A page that flags none is rejected, so a
// redesign or a challenge page fails the poll instead of passing as a quiet
// week.
func (Flow) Extract(page rfs.Page) ([]rfs.ExtractedItem, error) {
	doc, err := rfs.ParseHTML(page)
	if err != nil {
		return nil, fmt.Errorf("boomkat: parse page: %w", err)
	}

	var items []rfs.ExtractedItem
	seen := map[string]bool{}
	for _, flag := range dom.WithClass(doc, "page-block-header-flag") {
		if !strings.EqualFold(dom.Collapse(dom.Text(flag)), flagLabel) {
			continue
		}
		block := pageBlock(flag)
		if block == nil {
			return nil, fmt.Errorf("boomkat: %s flag is not inside a page block", flagLabel)
		}
		item, id, err := readPick(block, flag)
		if err != nil {
			return nil, err
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		items = append(items, item)
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("boomkat: no %s on the page", flagLabel)
	}
	return items, nil
}

// checkpoint is the announcement state the Flow owns. rfs stores it opaquely.
type checkpoint struct {
	Version   int      `json:"version"`
	Announced []string `json:"announced"`
}

// Evaluate announces each observed release that has not been announced yet,
// in page order. The first observation has nothing announced, so it announces
// the current pick. A release stays in the announced set after the homepage
// drops it, so an earlier pick that comes back is not announced again.
func (Flow) Evaluate(raw json.RawMessage, current []rfs.ExtractedItem) (rfs.AnnouncementDecision, error) {
	if len(current) == 0 {
		return rfs.AnnouncementDecision{}, fmt.Errorf("boomkat: no %s observed", flagLabel)
	}
	ids := make([]string, len(current))
	for i, item := range current {
		id, err := itemReleaseID(item.GUID)
		if err != nil {
			return rfs.AnnouncementDecision{}, err
		}
		ids[i] = id
	}

	announced := map[string]bool{}
	if len(raw) > 0 {
		state, err := decodeCheckpoint(raw)
		if err != nil {
			return rfs.AnnouncementDecision{}, err
		}
		for _, id := range state.Announced {
			announced[id] = true
		}
	}

	var announcements []rfs.ExtractedItem
	for i, item := range current {
		if announced[ids[i]] {
			continue
		}
		announced[ids[i]] = true
		announcements = append(announcements, item)
	}
	encoded, err := json.Marshal(checkpoint{Version: CheckpointVersion, Announced: slices.Sorted(maps.Keys(announced))})
	if err != nil {
		return rfs.AnnouncementDecision{}, err
	}
	return rfs.AnnouncementDecision{Checkpoint: encoded, Announcements: announcements}, nil
}

// pageBlock returns the page block that holds a header flag: the flag's
// nearest ancestor that names a block.
func pageBlock(flag *html.Node) *html.Node {
	for node := flag.Parent; node != nil; node = node.Parent {
		if node.Type == html.ElementNode && dom.Attr(node, "data-page-block-name") != "" {
			return node
		}
	}
	return nil
}

// readPick reads the release a Record Of The Week block names, and its ID.
// Every release link in the block must lead to the same release. The artist is
// the text of the link to that release's artist page. The title is the text of
// the first release link that has any: artwork links have none, and the Read
// More button comes last, so a block whose first titled link is that button
// has lost its title. The description is the block's text outside links and
// the flag, in page order: the formats line and the review excerpt.
func readPick(block, flag *html.Node) (rfs.ExtractedItem, string, error) {
	var (
		pick  release
		title string
		found bool
	)
	names := map[string]string{}
	for _, anchor := range dom.Elements(block, "a") {
		if within(anchor, flag) {
			continue
		}
		path, ok := sitePath(dom.Attr(anchor, "href"))
		if !ok {
			continue
		}
		text := dom.Collapse(dom.Text(anchor))
		if names[path] == "" {
			names[path] = text
		}
		ref, ok := parseRelease(path)
		if !ok {
			continue
		}
		if !found {
			pick, found = ref, true
		} else if ref.id != pick.id {
			return rfs.ExtractedItem{}, "", fmt.Errorf("boomkat: %s block links releases %s and %s", flagLabel, pick.id, ref.id)
		}
		if title == "" {
			title = text
		}
	}
	if !found {
		return rfs.ExtractedItem{}, "", fmt.Errorf("boomkat: %s block links no release", flagLabel)
	}
	if title == "" || strings.EqualFold(title, readMore) {
		return rfs.ExtractedItem{}, "", fmt.Errorf("boomkat: %s block has no title for release %s", flagLabel, pick.id)
	}
	artist := names["/artists/"+pick.artist]
	if artist == "" {
		return rfs.ExtractedItem{}, "", fmt.Errorf("boomkat: %s block has no artist for release %s", flagLabel, pick.id)
	}
	return rfs.ExtractedItem{
		GUID:        guidPrefix + pick.id,
		Title:       artist + " — " + title,
		Link:        "https://" + siteHost + pick.path,
		Description: blockText(block, flag),
	}, pick.id, nil
}

// blockText joins the text of the block's paragraphs that hold no link,
// outside the flag, in page order. A paragraph with a link is a name or a
// button, never review text.
func blockText(block, flag *html.Node) string {
	var parts []string
	for _, paragraph := range dom.Elements(block, "p") {
		if within(paragraph, flag) || len(dom.Elements(paragraph, "a")) > 0 {
			continue
		}
		if text := dom.Collapse(dom.Text(paragraph)); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " — ")
}

// within reports whether node is ancestor or lies beneath it.
func within(node, ancestor *html.Node) bool {
	for ; node != nil; node = node.Parent {
		if node == ancestor {
			return true
		}
	}
	return false
}

// release is one release link: the release's ID, its artist's slug, and the
// path of its page.
type release struct {
	id, artist, path string
}

func parseRelease(path string) (release, bool) {
	match := releasePath.FindStringSubmatch(path)
	if match == nil {
		return release{}, false
	}
	return release{id: match[2], artist: match[1], path: path}, true
}

// sitePath returns the escaped path of a link on boomkat.com, relative or
// absolute, without its query or fragment. A link to another host, or over
// another scheme than HTTPS, is not the site's.
func sitePath(href string) (string, bool) {
	trimmed := strings.TrimSpace(href)
	if trimmed == "" {
		return "", false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", false
	}
	if parsed.Host != "" && parsed.Host != siteHost {
		return "", false
	}
	if parsed.Scheme != "" && parsed.Scheme != "https" {
		return "", false
	}
	return parsed.EscapedPath(), true
}

// itemReleaseID recovers the release ID Extract embedded in an item GUID.
func itemReleaseID(guid string) (string, error) {
	id, ok := strings.CutPrefix(guid, guidPrefix)
	if !ok || !releaseID.MatchString(id) {
		return "", fmt.Errorf("boomkat: unexpected item GUID %q", guid)
	}
	return id, nil
}

func decodeCheckpoint(raw json.RawMessage) (checkpoint, error) {
	var state checkpoint
	if err := json.Unmarshal(raw, &state); err != nil {
		return checkpoint{}, fmt.Errorf("boomkat: decode checkpoint: %w", err)
	}
	if state.Version != CheckpointVersion {
		return checkpoint{}, fmt.Errorf("boomkat: unsupported checkpoint version %d", state.Version)
	}
	if len(state.Announced) == 0 {
		return checkpoint{}, fmt.Errorf("boomkat: checkpoint has no releases")
	}
	for _, id := range state.Announced {
		if !releaseID.MatchString(id) {
			return checkpoint{}, fmt.Errorf("boomkat: invalid checkpoint release %q", id)
		}
	}
	return state, nil
}
