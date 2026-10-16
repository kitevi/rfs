// Package visionario extracts new films from the Visionario archive.
//
// Cinema Visionario and Cinema Centrale in Udine share visionario.movie.
// The archive at /films/ lists film pages newest first, in the HTML of the
// first response. Centrale is closed for renovation, so current showtimes
// name Visionario; a showtime row that names Centrale is included when one
// appears. The homepage day switcher is unused: its programming endpoint
// returns an empty body.
//
// The first complete observation remembers every film on the page and
// publishes nothing. Later observations announce films that were not on
// that page, after reading each new film page. A prima visione or anteprima
// is announced once. An evento is remembered and left unpublished. The
// opening date stays in the description; the feed date is when rfs first
// saw the page.
package visionario

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"

	"golang.org/x/net/html"

	"github.com/kitevi/rfs/internal/rfs"
)

const (
	// PageURL is the film archive rfs polls. Page 1 is the newest films.
	PageURL = "https://visionario.movie/films/"

	// HumanURL is the cinema site a reader opens from the feed.
	HumanURL = "https://visionario.movie/"

	// siteHost is the only host whose film links are accepted.
	siteHost = "visionario.movie"

	// ExtractVersion is the derivation version for this Flow. Bump it whenever
	// Extract's output can change for a fixed page.
	ExtractVersion = 1

	// CheckpointVersion is the announcement checkpoint schema version.
	CheckpointVersion = 1

	// guidPrefix makes a film's slug recoverable from its GUID and keeps
	// identities unique across Sources.
	guidPrefix = "visionario:film:"

	kindRelease = "release"
	kindEvent   = "event"
)

// filmPath matches a film permalink. Archive pagination (/films/page/2/) does
// not match, so the next-page link cannot become an item.
var filmPath = regexp.MustCompile(`^/films/([a-z0-9-]+)/$`)

// openingLine matches the programmed opening date on a film page, including
// the abbreviated Italian month the site prints.
var openingLine = regexp.MustCompile(`(?i)^dal\s+\d{1,2}\s+(gen|feb|mar|apr|mag|giu|lug|ago|set|ott|nov|dic)\s+\d{4}$`)

// Flow extracts film links and decides which are new releases.
type Flow struct{}

var (
	_ rfs.AnnouncementFlow         = Flow{}
	_ rfs.AnnouncementIdentityFlow = Flow{}
)

// Version reports the derivation version; rfs re-fetches the full page when
// the stored version differs rather than trusting a conditional answer.
func (Flow) Version() int { return ExtractVersion }

// Extract returns every film article on the archive page, newest first.
// Repeated links to one film yield a single item. The title comes from the
// article heading, so an image link to the same film does not replace it.
func (Flow) Extract(page rfs.Page) ([]rfs.ExtractedItem, error) {
	doc, err := rfs.ParseHTML(page)
	if err != nil {
		return nil, fmt.Errorf("visionario: parse page: %w", err)
	}

	var items []rfs.ExtractedItem
	seen := map[string]bool{}
	for _, article := range elementsWithClass(doc, "type-films") {
		anchor := titleAnchor(article)
		if anchor == nil {
			return nil, fmt.Errorf("visionario: film article has no title link")
		}
		slug, ok := filmSlug(attr(anchor, "href"))
		if !ok {
			return nil, fmt.Errorf("visionario: film article links to %q", attr(anchor, "href"))
		}
		if seen[slug] {
			continue
		}
		title := collapse(textContent(anchor))
		if title == "" {
			return nil, fmt.Errorf("visionario: film %s has no title", slug)
		}
		seen[slug] = true
		items = append(items, rfs.ExtractedItem{
			GUID:  guidPrefix + slug,
			Title: title,
			Link:  filmURL(slug),
		})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("visionario: no films")
	}
	return items, nil
}

// checkpoint is the announcement state the Flow owns. rfs stores it opaquely.
// Seen includes the silent baseline and events that were not published, so a
// later poll cannot announce them.
type checkpoint struct {
	Version int      `json:"version"`
	Seen    []string `json:"seen"`
}

// filmNote is carried on an item from ResolveIdentity to Evaluate, then
// removed before the item is published.
type filmNote struct {
	Kind    string   `json:"kind"`
	Opening string   `json:"opening,omitempty"`
	Halls   []string `json:"halls,omitempty"`
}

// PlanIdentities reports films whose pages still need reading. The first
// observation has no checkpoint and publishes nothing, so it fetches no film
// pages. Later observations fetch only films the checkpoint has not seen.
func (Flow) PlanIdentities(raw json.RawMessage, current []rfs.ExtractedItem) ([]rfs.ExtractedItem, []rfs.ExtractedItem, error) {
	seen := map[string]bool{}
	if len(raw) > 0 {
		state, err := decodeCheckpoint(raw)
		if err != nil {
			return nil, nil, err
		}
		for _, slug := range state.Seen {
			if !validSlug(slug) {
				return nil, nil, fmt.Errorf("visionario: invalid checkpoint slug %q", slug)
			}
			seen[slug] = true
		}
	}

	ready := make([]rfs.ExtractedItem, 0, len(current))
	var pending []rfs.ExtractedItem
	observed := map[string]bool{}
	for _, item := range current {
		slug, err := itemSlug(item.GUID)
		if err != nil {
			return nil, nil, err
		}
		if observed[slug] {
			continue
		}
		observed[slug] = true
		ready = append(ready, item)
		if len(raw) > 0 && !seen[slug] {
			pending = append(pending, item)
		}
	}
	if len(ready) == 0 {
		return nil, nil, fmt.Errorf("visionario: no films observed")
	}
	return ready, pending, nil
}

// IdentityURL is the film page for one pending item, built from its slug.
func (Flow) IdentityURL(item rfs.ExtractedItem) (string, error) {
	slug, err := itemSlug(item.GUID)
	if err != nil {
		return "", err
	}
	return filmURL(slug), nil
}

// ResolveIdentity reads the badge, opening date, and halls from a film page.
// The note on Metadata tells Evaluate whether to announce the film; Evaluate
// removes it before publishing.
func (Flow) ResolveIdentity(page rfs.Page, item rfs.ExtractedItem) (rfs.ExtractedItem, error) {
	if _, err := itemSlug(item.GUID); err != nil {
		return rfs.ExtractedItem{}, err
	}
	note, err := classifyPage(page)
	if err != nil {
		return rfs.ExtractedItem{}, err
	}
	encoded, err := json.Marshal(note)
	if err != nil {
		return rfs.ExtractedItem{}, fmt.Errorf("visionario: encode film note: %w", err)
	}
	item.Metadata = string(encoded)
	return item, nil
}

// Evaluate decides which observed films to announce. The first observation
// stores every slug and publishes nothing. Later observations announce each
// unseen release once, in archive order, and remember events without
// publishing them. A film that leaves the archive stays remembered.
func (Flow) Evaluate(raw json.RawMessage, current []rfs.ExtractedItem) (rfs.AnnouncementDecision, error) {
	films, err := observedFilms(current)
	if err != nil {
		return rfs.AnnouncementDecision{}, err
	}
	if len(raw) == 0 {
		slugs := make([]string, len(films))
		for i, film := range films {
			slugs[i] = film.slug
		}
		encoded, err := encodeCheckpoint(checkpoint{Version: CheckpointVersion, Seen: sortedSlugs(setOf(slugs))})
		if err != nil {
			return rfs.AnnouncementDecision{}, err
		}
		return rfs.AnnouncementDecision{Checkpoint: encoded}, nil
	}

	state, err := decodeCheckpoint(raw)
	if err != nil {
		return rfs.AnnouncementDecision{}, err
	}
	seen := map[string]bool{}
	for _, slug := range state.Seen {
		if !validSlug(slug) {
			return rfs.AnnouncementDecision{}, fmt.Errorf("visionario: invalid checkpoint slug %q", slug)
		}
		seen[slug] = true
	}

	var announcements []rfs.ExtractedItem
	for _, film := range films {
		if seen[film.slug] {
			continue
		}
		note, err := takeNote(film.item)
		if err != nil {
			return rfs.AnnouncementDecision{}, err
		}
		seen[film.slug] = true
		if note.Kind == kindEvent {
			continue
		}
		item := film.item
		item.Description = filmDescription(note)
		item.Metadata = ""
		item.Link = filmURL(film.slug)
		announcements = append(announcements, item)
	}
	encoded, err := encodeCheckpoint(checkpoint{Version: CheckpointVersion, Seen: sortedSlugs(seen)})
	if err != nil {
		return rfs.AnnouncementDecision{}, err
	}
	return rfs.AnnouncementDecision{Checkpoint: encoded, Announcements: announcements}, nil
}

// observed is one film from an observation, in archive order.
type observed struct {
	slug string
	item rfs.ExtractedItem
}

// observedFilms validates an observation and returns it in page order.
// Duplicate slugs keep the first item, matching Extract's precedence.
func observedFilms(items []rfs.ExtractedItem) ([]observed, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("visionario: no films observed")
	}
	seen := map[string]bool{}
	films := make([]observed, 0, len(items))
	for _, item := range items {
		slug, err := itemSlug(item.GUID)
		if err != nil {
			return nil, err
		}
		if seen[slug] {
			continue
		}
		seen[slug] = true
		films = append(films, observed{slug: slug, item: item})
	}
	return films, nil
}

// classifyPage reads a film page into the note Evaluate consumes. A release
// without an opening date, or a badge that is neither a release nor an event,
// fails the poll so a layout change is not published as a film.
func classifyPage(page rfs.Page) (filmNote, error) {
	doc, err := rfs.ParseHTML(page)
	if err != nil {
		return filmNote{}, fmt.Errorf("visionario: parse film page: %w", err)
	}
	badge, err := badgeFrom(doc)
	if err != nil {
		return filmNote{}, err
	}
	kind, err := classifyBadge(badge)
	if err != nil {
		return filmNote{}, err
	}
	opening, err := openingFrom(doc)
	if err != nil {
		return filmNote{}, err
	}
	if kind == kindRelease && opening == "" {
		return filmNote{}, fmt.Errorf("visionario: film page has no opening date")
	}
	note := filmNote{Kind: kind, Halls: hallsFrom(doc)}
	if kind == kindRelease {
		note.Opening = opening
	}
	return note, nil
}

func classifyBadge(badge string) (string, error) {
	switch {
	case strings.Contains(badge, "prima visione"), strings.Contains(badge, "anteprima"):
		return kindRelease, nil
	case strings.Contains(badge, "evento"):
		return kindEvent, nil
	default:
		return "", fmt.Errorf("visionario: unrecognized badge %q", badge)
	}
}

func badgeFrom(doc *html.Node) (string, error) {
	var found string
	for _, node := range elementsWithClass(doc, "primaVisione") {
		text := strings.ToLower(collapse(textContent(node)))
		if text == "" {
			continue
		}
		if found != "" && found != text {
			return "", fmt.Errorf("visionario: multiple badges")
		}
		found = text
	}
	if found == "" {
		return "", fmt.Errorf("visionario: film page has no badge")
	}
	return found, nil
}

func openingFrom(doc *html.Node) (string, error) {
	var found string
	for _, node := range elementsWithClass(doc, "inizio") {
		text := collapse(textContent(node))
		if text == "" {
			continue
		}
		if !openingLine.MatchString(text) {
			return "", fmt.Errorf("visionario: unrecognized opening %q", text)
		}
		if found != "" && found != text {
			return "", fmt.Errorf("visionario: multiple opening dates")
		}
		found = text
	}
	return found, nil
}

func hallsFrom(doc *html.Node) []string {
	var halls []string
	seen := map[string]bool{}
	for _, node := range elementsWithClass(doc, "innerInfo") {
		name := strings.TrimSpace(collapse(textContent(node)))
		if name == "Cinema Centrale" {
			name = "Centrale"
		}
		if name != "Visionario" && name != "Centrale" {
			continue
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		halls = append(halls, name)
	}
	return halls
}

func filmDescription(note filmNote) string {
	if len(note.Halls) == 0 {
		return note.Opening
	}
	return note.Opening + ", " + strings.Join(note.Halls, ", ")
}

func takeNote(item rfs.ExtractedItem) (filmNote, error) {
	if item.Metadata == "" {
		return filmNote{}, fmt.Errorf("visionario: film %s was not classified", item.GUID)
	}
	var note filmNote
	if err := json.Unmarshal([]byte(item.Metadata), &note); err != nil {
		return filmNote{}, fmt.Errorf("visionario: decode film note: %w", err)
	}
	switch note.Kind {
	case kindRelease:
		if !openingLine.MatchString(note.Opening) {
			return filmNote{}, fmt.Errorf("visionario: film %s has no opening date", item.GUID)
		}
		for _, hall := range note.Halls {
			if hall != "Visionario" && hall != "Centrale" {
				return filmNote{}, fmt.Errorf("visionario: film %s has unknown hall %q", item.GUID, hall)
			}
		}
	case kindEvent:
	default:
		return filmNote{}, fmt.Errorf("visionario: film %s has unknown kind %q", item.GUID, note.Kind)
	}
	return note, nil
}

func itemSlug(guid string) (string, error) {
	slug, ok := strings.CutPrefix(guid, guidPrefix)
	if !ok || !validSlug(slug) {
		return "", fmt.Errorf("visionario: unexpected item GUID %q", guid)
	}
	return slug, nil
}

func validSlug(slug string) bool {
	return filmPath.MatchString("/films/" + slug + "/")
}

func filmURL(slug string) string {
	return "https://" + siteHost + "/films/" + slug + "/"
}

// filmSlug parses a film link into its slug. Links that leave the Visionario
// host or that point at archive pagination are rejected.
func filmSlug(href string) (string, bool) {
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
	match := filmPath.FindStringSubmatch(parsed.Path)
	if match == nil {
		return "", false
	}
	return match[1], true
}

func titleAnchor(article *html.Node) *html.Node {
	for _, heading := range elementsWithClass(article, "entry-title") {
		if anchor := firstChildAnchor(heading); anchor != nil {
			return anchor
		}
	}
	return nil
}

func firstChildAnchor(root *html.Node) *html.Node {
	var found *html.Node
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if found != nil {
			return
		}
		if node.Type == html.ElementNode && node.Data == "a" {
			found = node
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return found
}

func elementsWithClass(root *html.Node, class string) []*html.Node {
	var found []*html.Node
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && hasClass(node, class) {
			found = append(found, node)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return found
}

func hasClass(node *html.Node, class string) bool {
	for _, token := range strings.Fields(attr(node, "class")) {
		if token == class {
			return true
		}
	}
	return false
}

func attr(node *html.Node, name string) string {
	for _, attribute := range node.Attr {
		if attribute.Key == name {
			return attribute.Val
		}
	}
	return ""
}

func textContent(node *html.Node) string {
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return builder.String()
}

func collapse(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func setOf(slugs []string) map[string]bool {
	set := make(map[string]bool, len(slugs))
	for _, slug := range slugs {
		set[slug] = true
	}
	return set
}

func sortedSlugs(set map[string]bool) []string {
	slugs := make([]string, 0, len(set))
	for slug := range set {
		slugs = append(slugs, slug)
	}
	slices.Sort(slugs)
	return slugs
}

func encodeCheckpoint(state checkpoint) (json.RawMessage, error) {
	encoded, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("visionario: encode checkpoint: %w", err)
	}
	return encoded, nil
}

func decodeCheckpoint(raw json.RawMessage) (checkpoint, error) {
	var state checkpoint
	if err := json.Unmarshal(raw, &state); err != nil {
		return checkpoint{}, fmt.Errorf("visionario: decode checkpoint: %w", err)
	}
	if state.Version != CheckpointVersion {
		return checkpoint{}, fmt.Errorf("visionario: unsupported checkpoint version %d", state.Version)
	}
	if len(state.Seen) == 0 {
		return checkpoint{}, fmt.Errorf("visionario: checkpoint has no films")
	}
	return state, nil
}
