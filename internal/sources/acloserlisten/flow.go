// Package acloserlisten extracts A Closer Listen's recommended releases.
//
// The recommendations page is a WordPress page the site rewrites wholesale:
// whole genre sections are replaced at once, so a single poll can carry many
// new albums. The Flow treats each embedded Bandcamp player as one
// recommendation. Its Bandcamp album ID is the stable identity and its artist
// and title come from the player's own metadata, so editorial rewording of the
// prose can neither duplicate nor rename a published item.
package acloserlisten

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"github.com/kitevi/rfs/internal/rfs"
)

const (
	// PageID is the WordPress page the recommendations live on. The same API
	// serves the site's other pages, so extraction rejects them rather than
	// publishing the wrong list.
	PageID = 395

	// PageURL is the public WordPress.com REST response for the page, reduced
	// to the fields the Flow reads. The site's own HTML answers ordinary
	// clients with an anti-bot challenge, and the page carries no feed.
	PageURL = "https://public-api.wordpress.com/rest/v1.1/sites/acloserlisten.com/posts/395?fields=ID,content"

	// HumanURL is the page a reader opens to browse the same recommendations.
	HumanURL = "https://acloserlisten.com/recommendations/"

	// ExtractVersion is the derivation version for this Flow. Bump it whenever
	// Extract's output can change for a fixed page.
	ExtractVersion = 1

	// CheckpointVersion is the announcement checkpoint schema version.
	CheckpointVersion = 1

	// guidPrefix makes an album's Bandcamp ID recoverable from its GUID and
	// keeps identities unique across Sources.
	guidPrefix = "acloserlisten:bandcamp:album:"

	// playerURLPrefix and playerURLSuffix build a Bandcamp player URL from a
	// validated album ID. The page's own iframe src is never fetched verbatim.
	playerURLPrefix = "https://bandcamp.com/EmbeddedPlayer/v=2/album="
	playerURLSuffix = "/size=large/artwork=small/"

	// playerHost and playerAltHost are the only hosts whose embedded players
	// are accepted.
	playerHost    = "bandcamp.com"
	playerAltHost = "www.bandcamp.com"

	// playerPathPrefix is the Bandcamp embedded player path.
	playerPathPrefix = "/EmbeddedPlayer/"
)

// Flow extracts embedded album recommendations and decides which are new.
type Flow struct{}

// Version reports the derivation version; rfs re-extracts the full page when
// the stored version differs rather than trusting a conditional answer.
func (Flow) Version() int { return ExtractVersion }

// Extract returns one item per embedded Bandcamp album in page order. Titles
// arrive from EnrichAnnouncement, which reads the player's own metadata;
// Extract settles identity, genre, and the shared page link only.
func (Flow) Extract(page rfs.Page) ([]rfs.ExtractedItem, error) {
	var response struct {
		ID      *int    `json:"ID"`
		Content *string `json:"content"`
	}
	if err := json.Unmarshal(page, &response); err != nil {
		return nil, fmt.Errorf("acloserlisten: decode page: %w", err)
	}
	if response.ID == nil || *response.ID != PageID {
		return nil, fmt.Errorf("acloserlisten: unexpected page identity")
	}
	if response.Content == nil {
		return nil, fmt.Errorf("acloserlisten: page has no content")
	}
	doc, err := rfs.ParseHTML(rfs.Page(*response.Content))
	if err != nil {
		return nil, fmt.Errorf("acloserlisten: parse page: %w", err)
	}
	albums := embeddedAlbums(doc)
	if len(albums) == 0 {
		return nil, fmt.Errorf("acloserlisten: no embedded albums found")
	}
	items := make([]rfs.ExtractedItem, 0, len(albums))
	for _, album := range albums {
		items = append(items, rfs.ExtractedItem{
			GUID:        guidPrefix + album.id,
			Link:        HumanURL,
			Description: description(album.genre),
		})
	}
	return items, nil
}

// album is one embedded recommendation from the page, in document order.
type album struct {
	id    string
	genre string
}

// embeddedAlbums walks the content in document order, tracking the most recent
// genre heading and collecting unique Bandcamp album IDs.
func embeddedAlbums(doc *html.Node) []album {
	var found []album
	seen := map[string]bool{}
	genre := ""
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode {
			switch node.Data {
			case "strong":
				if label := genreLabel(node); label != "" {
					genre = label
				}
			case "iframe":
				if id, ok := embeddedAlbumID(attr(node, "src")); ok && !seen[id] {
					seen[id] = true
					found = append(found, album{id: id, genre: genre})
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	return found
}

// genreLabel reports the section a bold lead-in introduces. Sections are
// written as a bold genre followed by a line break, so a bold run without a
// line break is prose emphasis, not a genre.
func genreLabel(strong *html.Node) string {
	var label strings.Builder
	for child := strong.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == "br" {
			return strings.TrimSpace(label.String())
		}
		if child.Type == html.TextNode {
			label.WriteString(child.Data)
		}
	}
	return ""
}

// embeddedAlbumID validates a Bandcamp embedded player URL and returns its
// canonical album ID. Album IDs are the identity of a recommendation, so only
// the documented player path on Bandcamp's own hosts is accepted.
func embeddedAlbumID(src string) (string, bool) {
	trimmed := strings.TrimSpace(src)
	if trimmed == "" {
		return "", false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", false
	}
	if parsed.Scheme != "" && parsed.Scheme != "https" {
		return "", false
	}
	if parsed.Host != playerHost && parsed.Host != playerAltHost {
		return "", false
	}
	if !strings.HasPrefix(parsed.Path, playerPathPrefix) {
		return "", false
	}
	for _, segment := range strings.Split(parsed.Path, "/") {
		value, ok := strings.CutPrefix(segment, "album=")
		if !ok {
			continue
		}
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id <= 0 {
			return "", false
		}
		return strconv.FormatInt(id, 10), true
	}
	return "", false
}

func attr(node *html.Node, name string) string {
	for _, a := range node.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

// description renders the genre a recommendation appeared under as plain text:
// the feed escapes it and no upstream markup is copied.
func description(genre string) string {
	if genre == "" {
		return ""
	}
	return "Genre: " + genre
}

// checkpoint is the announcement state the Flow owns. rfs stores it opaquely.
// Announced albums are kept for the life of the feed: the page is replaced
// wholesale, and an album that returns in a later revision must not be
// announced twice.
type checkpoint struct {
	Version   int      `json:"version"`
	Announced []string `json:"announced"`
}

// Evaluate decides which observed albums to announce. Every album embedded in
// the first complete observation is published, so a new subscriber starts with
// the list as it stands. Later observations publish only albums that have never
// been announced, in page order, so a whole-page replacement can release many
// entries in one poll without repeating an earlier one.
func (Flow) Evaluate(raw json.RawMessage, current []rfs.ExtractedItem) (rfs.AnnouncementDecision, error) {
	observed, err := observedAlbums(current)
	if err != nil {
		return rfs.AnnouncementDecision{}, err
	}
	announced := map[string]bool{}
	if len(raw) > 0 {
		var state checkpoint
		if err := json.Unmarshal(raw, &state); err != nil {
			return rfs.AnnouncementDecision{}, fmt.Errorf("acloserlisten: decode checkpoint: %w", err)
		}
		if state.Version != CheckpointVersion {
			return rfs.AnnouncementDecision{}, fmt.Errorf("acloserlisten: unsupported checkpoint version %d", state.Version)
		}
		for _, id := range state.Announced {
			announced[id] = true
		}
	}
	var announcements []rfs.ExtractedItem
	for _, album := range observed {
		if announced[album.id] {
			continue
		}
		announced[album.id] = true
		announcements = append(announcements, album.item)
	}
	encoded, err := encodeCheckpoint(checkpoint{Version: CheckpointVersion, Announced: announcedIDs(announced)})
	if err != nil {
		return rfs.AnnouncementDecision{}, err
	}
	return rfs.AnnouncementDecision{Checkpoint: encoded, Announcements: announcements}, nil
}

// observed is one validated album from an observation, in page order.
type observed struct {
	id   string
	item rfs.ExtractedItem
}

// observedAlbums validates an observation and returns it in page order.
// Duplicate IDs keep the first item, matching Extract's precedence.
func observedAlbums(items []rfs.ExtractedItem) ([]observed, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("acloserlisten: no albums observed")
	}
	seen := map[string]bool{}
	albums := make([]observed, 0, len(items))
	for _, item := range items {
		id, err := itemAlbumID(item.GUID)
		if err != nil {
			return nil, err
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		albums = append(albums, observed{id: id, item: item})
	}
	return albums, nil
}

// itemAlbumID recovers the canonical album ID Extract embedded in an item GUID.
func itemAlbumID(guid string) (string, error) {
	raw, ok := strings.CutPrefix(guid, guidPrefix)
	if !ok {
		return "", fmt.Errorf("acloserlisten: unexpected item GUID %q", guid)
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return "", fmt.Errorf("acloserlisten: unexpected item GUID %q", guid)
	}
	return strconv.FormatInt(id, 10), nil
}

func announcedIDs(set map[string]bool) []string {
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// encodeCheckpoint serializes the Flow-owned announcement state for opaque
// storage by rfs.
func encodeCheckpoint(state checkpoint) (json.RawMessage, error) {
	encoded, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("acloserlisten: encode checkpoint: %w", err)
	}
	return encoded, nil
}

// AnnouncementEnrichmentURL names the Bandcamp player that carries an album's
// artist and title. The URL is built from the validated album ID, never from
// the page's own iframe markup.
func (Flow) AnnouncementEnrichmentURL(item rfs.ExtractedItem) (string, error) {
	id, err := itemAlbumID(item.GUID)
	if err != nil {
		return "", err
	}
	return playerURLPrefix + id + playerURLSuffix, nil
}

// EnrichAnnouncement publishes the album's own name. The player serves the
// same release the page embedded, so the title does not depend on how the
// prose happens to introduce it, and a nameless player is an error rather than
// an entry with an invented title.
func (Flow) EnrichAnnouncement(page rfs.Page, item rfs.ExtractedItem) (rfs.ExtractedItem, error) {
	expected, err := itemAlbumID(item.GUID)
	if err != nil {
		return rfs.ExtractedItem{}, err
	}
	doc, err := rfs.ParseHTML(page)
	if err != nil {
		return rfs.ExtractedItem{}, fmt.Errorf("acloserlisten: parse player %s: %w", expected, err)
	}
	raw, ok := playerData(doc)
	if !ok {
		return rfs.ExtractedItem{}, fmt.Errorf("acloserlisten: player %s carries no metadata", expected)
	}
	var metadata struct {
		AlbumID *int64  `json:"album_id"`
		Artist  *string `json:"artist"`
		Title   *string `json:"album_title"`
	}
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return rfs.ExtractedItem{}, fmt.Errorf("acloserlisten: decode player %s: %w", expected, err)
	}
	if metadata.AlbumID == nil || strconv.FormatInt(*metadata.AlbumID, 10) != expected {
		return rfs.ExtractedItem{}, fmt.Errorf("acloserlisten: player %s serves a different album", expected)
	}
	artist := strings.TrimSpace(textValue(metadata.Artist))
	title := strings.TrimSpace(textValue(metadata.Title))
	if artist == "" || title == "" {
		return rfs.ExtractedItem{}, fmt.Errorf("acloserlisten: player %s has no artist and title", expected)
	}
	item.Title = artist + " — " + title
	return item, nil
}

// playerData returns the player metadata JSON. The HTML parser already decoded
// the attribute's entities, so the JSON is not unescaped a second time.
func playerData(node *html.Node) (string, bool) {
	if node.Type == html.ElementNode {
		if raw := attr(node, "data-player-data"); raw != "" {
			return raw, true
		}
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if raw, ok := playerData(child); ok {
			return raw, true
		}
	}
	return "", false
}

func textValue(text *string) string {
	if text == nil {
		return ""
	}
	return *text
}
