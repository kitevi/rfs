// Package onepiece extracts One Piece chapter releases from TCB Scans.
//
// The site publishes chapters on a schedule of its own and goes down often, so
// the Flow announces each chapter exactly once instead of deriving the feed
// from the latest page: the first complete observation publishes only the
// newest chapter, and later observations publish every chapter above that
// baseline that has not been announced yet, including releases that appeared
// while the site was unreachable.
package onepiece

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"github.com/kitevi/rfs/internal/rfs"
)

const (
	// PageURL is the One Piece chapter archive. The homepage only lists the
	// newest chapter of every series, so it cannot recover a release missed
	// while the site was down.
	PageURL = "https://tcbonepiecechapters.com/mangas/5/one-piece"

	// HumanURL is the page a reader opens to browse the same chapters.
	HumanURL = PageURL

	// siteHost is the only host whose chapter links are accepted.
	siteHost = "tcbonepiecechapters.com"

	// ExtractVersion is the derivation version for this Flow. Bump it whenever
	// Extract's output can change for a fixed page.
	ExtractVersion = 1

	// CheckpointVersion is the announcement checkpoint schema version.
	CheckpointVersion = 1

	// guidPrefix makes a chapter's number recoverable from its GUID and keeps
	// identities unique across Sources.
	guidPrefix = "one-piece:chapter:"
)

// chapterPath matches a main-series chapter link. The optional -review- suffix
// covers the site's occasional review-style URLs; spin-offs and other series
// use different slugs and never match.
var chapterPath = regexp.MustCompile(`^/chapters/\d+/one-piece-chapter-(\d+)(?:-review-\d+)?$`)

// headingChapter matches the chapter number inside an anchor's heading,
// tolerating extra text such as a color-spread note.
var headingChapter = regexp.MustCompile(`(?i)\bchapter\s+(\d+)\b`)

// Flow extracts One Piece chapter links and decides which are new.
type Flow struct{}

// Version reports the derivation version; rfs re-extracts the full page when
// the stored version differs rather than trusting a conditional answer.
func (Flow) Version() int { return ExtractVersion }

// Extract returns every main-series chapter the archive lists, newest first.
// The archive lists chapters back to chapter 1, so the announced set, not the
// page, decides what is new.
func (Flow) Extract(page rfs.Page) ([]rfs.ExtractedItem, error) {
	doc, err := rfs.ParseHTML(page)
	if err != nil {
		return nil, fmt.Errorf("onepiece: parse page: %w", err)
	}

	var items []rfs.ExtractedItem
	seen := map[int]bool{}
	for _, anchor := range anchors(doc) {
		number, link, ok := chapterRef(attr(anchor, "href"))
		if !ok || seen[number] {
			continue
		}
		heading, subtitle, err := chapterText(anchor, number)
		if err != nil {
			return nil, err
		}
		seen[number] = true
		items = append(items, rfs.ExtractedItem{
			GUID:  guidPrefix + strconv.Itoa(number),
			Title: chapterTitle(heading, subtitle),
			Link:  link,
		})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("onepiece: no chapter links found")
	}
	return items, nil
}

// checkpoint is the announcement state the Flow owns. rfs stores it opaquely.
type checkpoint struct {
	Version   int   `json:"version"`
	Initial   int   `json:"initial"`
	Announced []int `json:"announced"`
}

// Evaluate decides which observed chapters to announce. The first observation
// announces only its newest chapter and stores it as the baseline. Later
// observations announce every chapter above that baseline that has not been
// announced yet, in ascending order, so an outage spanning several releases
// publishes all of them exactly once. Chapters that disappear from the archive
// stay in the announced set and are never re-announced, and chapters at or
// below the baseline are ignored so a backfilled archive cannot flood the feed.
func (Flow) Evaluate(raw json.RawMessage, current []rfs.ExtractedItem) (rfs.AnnouncementDecision, error) {
	chapters, err := observedChapters(current)
	if err != nil {
		return rfs.AnnouncementDecision{}, err
	}
	if len(raw) == 0 {
		latest := chapters[len(chapters)-1]
		encoded, err := encodeCheckpoint(checkpoint{Version: CheckpointVersion, Initial: latest.number, Announced: []int{latest.number}})
		if err != nil {
			return rfs.AnnouncementDecision{}, err
		}
		return rfs.AnnouncementDecision{Checkpoint: encoded, Announcements: []rfs.ExtractedItem{latest.item}}, nil
	}

	var state checkpoint
	if err := json.Unmarshal(raw, &state); err != nil {
		return rfs.AnnouncementDecision{}, fmt.Errorf("onepiece: decode checkpoint: %w", err)
	}
	if state.Version != CheckpointVersion {
		return rfs.AnnouncementDecision{}, fmt.Errorf("onepiece: unsupported checkpoint version %d", state.Version)
	}
	if state.Initial <= 0 {
		return rfs.AnnouncementDecision{}, fmt.Errorf("onepiece: checkpoint has no initial chapter")
	}

	announced := make(map[int]bool, len(state.Announced))
	for _, number := range state.Announced {
		announced[number] = true
	}
	var announcements []rfs.ExtractedItem
	for _, chapter := range chapters {
		if chapter.number > state.Initial && !announced[chapter.number] {
			announcements = append(announcements, chapter.item)
			announced[chapter.number] = true
		}
	}
	state.Announced = sortedNumbers(announced)
	encoded, err := encodeCheckpoint(state)
	if err != nil {
		return rfs.AnnouncementDecision{}, err
	}
	return rfs.AnnouncementDecision{Checkpoint: encoded, Announcements: announcements}, nil
}

// observed is one validated chapter from an observation, ordered by number.
type observed struct {
	number int
	item   rfs.ExtractedItem
}

// observedChapters validates an observation and returns it ordered by chapter
// number. Duplicate numbers keep the first item, matching Extract's precedence.
func observedChapters(items []rfs.ExtractedItem) ([]observed, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("onepiece: no chapters observed")
	}
	byNumber := make(map[int]rfs.ExtractedItem, len(items))
	for _, item := range items {
		number, err := itemChapterNumber(item.GUID)
		if err != nil {
			return nil, err
		}
		if _, ok := byNumber[number]; !ok {
			byNumber[number] = item
		}
	}
	chapters := make([]observed, 0, len(byNumber))
	for number, item := range byNumber {
		chapters = append(chapters, observed{number: number, item: item})
	}
	sort.Slice(chapters, func(i, j int) bool { return chapters[i].number < chapters[j].number })
	return chapters, nil
}

// itemChapterNumber recovers the chapter number Extract embedded in an item
// GUID.
func itemChapterNumber(guid string) (int, error) {
	raw, ok := strings.CutPrefix(guid, guidPrefix)
	if !ok {
		return 0, fmt.Errorf("onepiece: unexpected item GUID %q", guid)
	}
	number, err := strconv.Atoi(raw)
	if err != nil || number <= 0 {
		return 0, fmt.Errorf("onepiece: unexpected item GUID %q", guid)
	}
	return number, nil
}

func sortedNumbers(set map[int]bool) []int {
	numbers := make([]int, 0, len(set))
	for number := range set {
		numbers = append(numbers, number)
	}
	sort.Ints(numbers)
	return numbers
}

// encodeCheckpoint serializes the Flow-owned announcement state for opaque
// storage by rfs.
func encodeCheckpoint(state checkpoint) (json.RawMessage, error) {
	encoded, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("onepiece: encode checkpoint: %w", err)
	}
	return encoded, nil
}

// anchors returns every anchor element in document order.
func anchors(n *html.Node) []*html.Node {
	var found []*html.Node
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == "a" {
			found = append(found, node)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(n)
	return found
}

func attr(n *html.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

// chapterRef parses a chapter link into its number and canonical HTTPS URL.
// Links that leave the TCB Scans host or that do not point at a main-series
// chapter are rejected, so a page mixing series cannot smuggle in a wrong link.
func chapterRef(href string) (int, string, bool) {
	trimmed := strings.TrimSpace(href)
	if trimmed == "" {
		return 0, "", false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return 0, "", false
	}
	if parsed.Host != "" && parsed.Host != siteHost {
		return 0, "", false
	}
	if parsed.Scheme != "" && parsed.Scheme != "https" {
		return 0, "", false
	}
	match := chapterPath.FindStringSubmatch(parsed.Path)
	if match == nil {
		return 0, "", false
	}
	number, err := strconv.Atoi(match[1])
	if err != nil || number <= 0 {
		return 0, "", false
	}
	return number, "https://" + siteHost + parsed.Path, true
}

// chapterText returns the anchor's heading and optional subtitle. Each chapter
// is rendered as an anchor holding a heading div and a subtitle div; the
// heading is the first div that names the expected chapter, so an unexpected
// extra element does not shift the text.
func chapterText(anchor *html.Node, number int) (string, string, error) {
	var texts []string
	for child := anchor.FirstChild; child != nil; child = child.NextSibling {
		if child.Type != html.ElementNode || child.Data != "div" {
			continue
		}
		if text := normalizeText(nodeText(child)); text != "" {
			texts = append(texts, text)
		}
	}
	for i, text := range texts {
		if !headingMatchesNumber(text, number) {
			continue
		}
		subtitle := ""
		if i+1 < len(texts) {
			subtitle = texts[i+1]
		}
		return text, subtitle, nil
	}
	if len(texts) == 0 {
		return "", "", fmt.Errorf("onepiece: chapter link has no heading")
	}
	return "", "", fmt.Errorf("onepiece: chapter %d heading %q does not match its link", number, texts[0])
}

func headingMatchesNumber(heading string, number int) bool {
	match := headingChapter.FindStringSubmatch(heading)
	if match == nil {
		return false
	}
	found, err := strconv.Atoi(match[1])
	return err == nil && found == number
}

func chapterTitle(heading, subtitle string) string {
	if subtitle == "" || strings.EqualFold(subtitle, heading) {
		return heading
	}
	return heading + " — " + subtitle
}

func normalizeText(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func nodeText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var b strings.Builder
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		b.WriteString(nodeText(child))
	}
	return b.String()
}
