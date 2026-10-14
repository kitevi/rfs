// Package acloserlisten extracts A Closer Listen's recommended releases.
//
// The recommendations page is a WordPress page the site rewrites wholesale.
// The usual revision is genre sections of Bandcamp album players. A revision
// may also embed a single track. An album player, and a track player that
// belongs to an album, are that album. A track with no album is its own
// release. YouTube and other players are not releases. Identity comes from
// the player, so editorial rewording can neither duplicate nor rename a
// published item.
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
	// Extract's output can change for a fixed page. Version 2 also extracts
	// track players; album-only pages still produce the same items.
	ExtractVersion = 2

	// CheckpointVersion is the announcement checkpoint schema version. Track
	// resolutions are an optional field on version 1 so an existing checkpoint
	// still loads.
	CheckpointVersion = 1

	// guidPrefix makes an album's Bandcamp ID recoverable from its GUID and
	// keeps identities unique across Sources.
	guidPrefix = "acloserlisten:bandcamp:album:"

	// trackGUIDPrefix is the identity of a Bandcamp track that does not belong
	// to an album. It does not share the album namespace.
	trackGUIDPrefix = "acloserlisten:bandcamp:track:"

	// trackCheckpointPrefix marks a standalone track in the announced set.
	// Bare numbers remain album IDs, which is what version-1 checkpoints store.
	trackCheckpointPrefix = "track:"

	// playerURLSuffix is the player size this Flow requests. The page's own
	// iframe src is never fetched verbatim.
	playerURLSuffix = "/size=large/artwork=small/"

	// playerHost and playerAltHost are the only hosts whose embedded players
	// are accepted.
	playerHost    = "bandcamp.com"
	playerAltHost = "www.bandcamp.com"

	// playerPathPrefix is the Bandcamp embedded player path.
	playerPathPrefix = "/EmbeddedPlayer/"
)

// releaseKind distinguishes an album player from a track-only player.
type releaseKind uint8

const (
	releaseAlbum releaseKind = iota
	releaseTrack
)

// release is one Bandcamp player identity derived from an iframe URL.
type release struct {
	kind releaseKind
	id   string
}

func (r release) guid() string {
	if r.kind == releaseTrack {
		return trackGUIDPrefix + r.id
	}
	return guidPrefix + r.id
}

// checkpointKey is the form stored in the announced set. Album keys stay bare
// numbers so a checkpoint written before track support still matches.
func (r release) checkpointKey() string {
	if r.kind == releaseTrack {
		return trackCheckpointPrefix + r.id
	}
	return r.id
}

func (r release) playerURL() string {
	kind := "album="
	if r.kind == releaseTrack {
		kind = "track="
	}
	return "https://bandcamp.com/EmbeddedPlayer/v=2/" + kind + r.id + playerURLSuffix
}

// Flow extracts embedded album recommendations and decides which are new.
type Flow struct{}

// Version reports the derivation version; rfs re-extracts the full page when
// the stored version differs rather than trusting a conditional answer.
func (Flow) Version() int { return ExtractVersion }

// Extract returns one item per embedded Bandcamp album or track-only player,
// in page order. An album URL that also names a track is the album. Titles
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
	found := embeddedReleases(doc)
	if len(found) == 0 {
		return nil, fmt.Errorf("acloserlisten: no embedded releases found")
	}
	items := make([]rfs.ExtractedItem, 0, len(found))
	for _, embedded := range found {
		items = append(items, rfs.ExtractedItem{
			GUID:        embedded.release.guid(),
			Link:        HumanURL,
			Description: description(embedded.genre),
		})
	}
	return items, nil
}

// embedded is one player from the page, in document order.
type embedded struct {
	release release
	genre   string
}

// embeddedReleases walks the content in document order, tracking the most
// recent genre heading and collecting unique Bandcamp players. An album URL
// wins over a track parameter in the same player, because that parameter only
// chooses the starting song.
func embeddedReleases(doc *html.Node) []embedded {
	var found []embedded
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
				if rel, ok := embeddedRelease(attr(node, "src")); ok && !seen[rel.guid()] {
					seen[rel.guid()] = true
					found = append(found, embedded{release: rel, genre: genre})
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

// embeddedRelease validates a Bandcamp embedded player URL and returns the
// release it identifies. Album IDs and standalone track IDs are the identity
// of a recommendation, so only the documented player path on Bandcamp's own
// hosts is accepted.
func embeddedRelease(src string) (release, bool) {
	trimmed := strings.TrimSpace(src)
	if trimmed == "" {
		return release{}, false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return release{}, false
	}
	if parsed.Scheme != "" && parsed.Scheme != "https" {
		return release{}, false
	}
	if parsed.Host != playerHost && parsed.Host != playerAltHost {
		return release{}, false
	}
	if !strings.HasPrefix(parsed.Path, playerPathPrefix) {
		return release{}, false
	}
	var albumID, trackID string
	sawAlbum := false
	trackInvalid := false
	for _, segment := range strings.Split(parsed.Path, "/") {
		if value, ok := strings.CutPrefix(segment, "album="); ok {
			sawAlbum = true
			id, err := canonicalID(value)
			if err != nil {
				return release{}, false
			}
			if albumID == "" {
				albumID = id
			}
			continue
		}
		value, ok := strings.CutPrefix(segment, "track=")
		if !ok {
			continue
		}
		id, err := canonicalID(value)
		if err != nil {
			trackInvalid = true
			continue
		}
		if trackID == "" {
			trackID = id
		}
	}
	if sawAlbum {
		return release{kind: releaseAlbum, id: albumID}, true
	}
	if trackID == "" || trackInvalid {
		return release{}, false
	}
	return release{kind: releaseTrack, id: trackID}, true
}

func canonicalID(raw string) (string, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return "", fmt.Errorf("not a positive id")
	}
	return strconv.FormatInt(id, 10), nil
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
// Announced releases are kept for the life of the feed: the page is replaced
// wholesale, and a release that returns in a later revision must not be
// announced twice. Tracks remembers which release a track player identified,
// so a later poll does not refetch a player it has already resolved.
type checkpoint struct {
	Version   int               `json:"version"`
	Announced []string          `json:"announced"`
	Tracks    map[string]string `json:"tracks,omitempty"`
}

// identityNote is carried on an item from ResolveIdentity to Evaluate, then
// removed before the item is published.
type identityNote struct {
	Track   string `json:"track"`
	Release string `json:"release"`
}

// Evaluate decides which observed releases to announce. Every release embedded
// in the first complete observation is published, so a new subscriber starts
// with the list as it stands. Later observations publish only releases that
// have never been announced, in page order, so a whole-page replacement can
// release many entries in one poll without repeating an earlier one.
func (Flow) Evaluate(raw json.RawMessage, current []rfs.ExtractedItem) (rfs.AnnouncementDecision, error) {
	tracks := map[string]string{}
	announced := map[string]bool{}
	if len(raw) > 0 {
		state, err := decodeCheckpoint(raw)
		if err != nil {
			return rfs.AnnouncementDecision{}, err
		}
		for id, key := range state.Tracks {
			if _, err := canonicalID(id); err != nil {
				return rfs.AnnouncementDecision{}, fmt.Errorf("acloserlisten: invalid resolved track %q", id)
			}
			if _, err := releaseFromCheckpointKey(key); err != nil {
				return rfs.AnnouncementDecision{}, err
			}
			tracks[id] = key
		}
		for _, id := range state.Announced {
			rel, err := releaseFromCheckpointKey(id)
			if err != nil {
				return rfs.AnnouncementDecision{}, err
			}
			announced[rel.checkpointKey()] = true
		}
	}
	stripped := make([]rfs.ExtractedItem, len(current))
	for i, item := range current {
		note, ok, err := takeIdentityNote(item.Metadata)
		if err != nil {
			return rfs.AnnouncementDecision{}, err
		}
		if ok {
			if err := rememberTrack(tracks, note); err != nil {
				return rfs.AnnouncementDecision{}, err
			}
			item.Metadata = ""
		}
		stripped[i] = item
	}
	observed, err := observedReleases(stripped)
	if err != nil {
		return rfs.AnnouncementDecision{}, err
	}
	var announcements []rfs.ExtractedItem
	for _, candidate := range observed {
		if announced[candidate.release.checkpointKey()] {
			continue
		}
		announced[candidate.release.checkpointKey()] = true
		announcements = append(announcements, candidate.item)
	}
	encoded, err := encodeCheckpoint(checkpoint{
		Version:   CheckpointVersion,
		Announced: announcedIDs(announced),
		Tracks:    tracksOrNil(tracks),
	})
	if err != nil {
		return rfs.AnnouncementDecision{}, err
	}
	return rfs.AnnouncementDecision{Checkpoint: encoded, Announcements: announcements}, nil
}

// observed is one validated release from an observation, in page order.
type observed struct {
	release release
	item    rfs.ExtractedItem
}

// observedReleases validates an observation and returns it in page order.
// Duplicate identities keep the first item, matching Extract's precedence.
func observedReleases(items []rfs.ExtractedItem) ([]observed, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("acloserlisten: no releases observed")
	}
	seen := map[string]bool{}
	releases := make([]observed, 0, len(items))
	for _, item := range items {
		rel, err := itemRelease(item.GUID)
		if err != nil {
			return nil, err
		}
		if seen[rel.guid()] {
			continue
		}
		seen[rel.guid()] = true
		releases = append(releases, observed{release: rel, item: item})
	}
	return releases, nil
}

// itemRelease recovers the release Extract embedded in an item GUID.
func itemRelease(guid string) (release, error) {
	if raw, ok := strings.CutPrefix(guid, trackGUIDPrefix); ok {
		id, err := canonicalID(raw)
		if err != nil {
			return release{}, fmt.Errorf("acloserlisten: unexpected item GUID %q", guid)
		}
		return release{kind: releaseTrack, id: id}, nil
	}
	raw, ok := strings.CutPrefix(guid, guidPrefix)
	if !ok {
		return release{}, fmt.Errorf("acloserlisten: unexpected item GUID %q", guid)
	}
	id, err := canonicalID(raw)
	if err != nil {
		return release{}, fmt.Errorf("acloserlisten: unexpected item GUID %q", guid)
	}
	return release{kind: releaseAlbum, id: id}, nil
}

func releaseFromCheckpointKey(key string) (release, error) {
	if raw, ok := strings.CutPrefix(key, trackCheckpointPrefix); ok {
		id, err := canonicalID(raw)
		if err != nil {
			return release{}, fmt.Errorf("acloserlisten: unexpected checkpoint entry %q", key)
		}
		return release{kind: releaseTrack, id: id}, nil
	}
	id, err := canonicalID(key)
	if err != nil {
		return release{}, fmt.Errorf("acloserlisten: unexpected checkpoint entry %q", key)
	}
	return release{kind: releaseAlbum, id: id}, nil
}

func announcedIDs(set map[string]bool) []string {
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func tracksOrNil(tracks map[string]string) map[string]string {
	if len(tracks) == 0 {
		return nil
	}
	return tracks
}

func decodeCheckpoint(raw json.RawMessage) (checkpoint, error) {
	var state checkpoint
	if err := json.Unmarshal(raw, &state); err != nil {
		return checkpoint{}, fmt.Errorf("acloserlisten: decode checkpoint: %w", err)
	}
	if state.Version != CheckpointVersion {
		return checkpoint{}, fmt.Errorf("acloserlisten: unsupported checkpoint version %d", state.Version)
	}
	return state, nil
}

func takeIdentityNote(metadata string) (identityNote, bool, error) {
	if metadata == "" {
		return identityNote{}, false, nil
	}
	var note identityNote
	if err := json.Unmarshal([]byte(metadata), &note); err != nil {
		return identityNote{}, false, fmt.Errorf("acloserlisten: decode identity note: %w", err)
	}
	if note.Track == "" || note.Release == "" {
		return identityNote{}, false, fmt.Errorf("acloserlisten: incomplete identity note")
	}
	return note, true, nil
}

func rememberTrack(tracks map[string]string, note identityNote) error {
	trackID, err := canonicalID(note.Track)
	if err != nil {
		return fmt.Errorf("acloserlisten: invalid resolved track %q", note.Track)
	}
	rel, err := releaseFromCheckpointKey(note.Release)
	if err != nil {
		return err
	}
	if rel.kind == releaseTrack && rel.id != trackID {
		return fmt.Errorf("acloserlisten: track %s resolved to a different track", trackID)
	}
	tracks[trackID] = rel.checkpointKey()
	return nil
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

// PlanIdentities rewrites track players whose release was already resolved and
// reports the rest. Album players are already final, so the usual genre-section
// page performs no extra fetches.
func (Flow) PlanIdentities(raw json.RawMessage, current []rfs.ExtractedItem) ([]rfs.ExtractedItem, []rfs.ExtractedItem, error) {
	known := map[string]string{}
	if len(raw) > 0 {
		state, err := decodeCheckpoint(raw)
		if err != nil {
			return nil, nil, err
		}
		for id, key := range state.Tracks {
			if _, err := canonicalID(id); err != nil {
				return nil, nil, fmt.Errorf("acloserlisten: invalid resolved track %q", id)
			}
			if _, err := releaseFromCheckpointKey(key); err != nil {
				return nil, nil, err
			}
			known[id] = key
		}
	}
	seen := map[string]bool{}
	ready := make([]rfs.ExtractedItem, 0, len(current))
	var pending []rfs.ExtractedItem
	for _, item := range current {
		rel, err := itemRelease(item.GUID)
		if err != nil {
			return nil, nil, err
		}
		unresolved := false
		if rel.kind == releaseTrack {
			if key, ok := known[rel.id]; ok {
				rel, err = releaseFromCheckpointKey(key)
				if err != nil {
					return nil, nil, err
				}
				item.GUID = rel.guid()
			} else {
				unresolved = true
			}
		}
		if seen[item.GUID] {
			continue
		}
		seen[item.GUID] = true
		ready = append(ready, item)
		if unresolved {
			pending = append(pending, item)
		}
	}
	if len(ready) == 0 {
		return nil, nil, fmt.Errorf("acloserlisten: no releases observed")
	}
	return ready, pending, nil
}

// IdentityURL names the track player that can say whether this embed is an
// album or a standalone track.
func (Flow) IdentityURL(item rfs.ExtractedItem) (string, error) {
	rel, err := itemRelease(item.GUID)
	if err != nil {
		return "", err
	}
	if rel.kind != releaseTrack {
		return "", fmt.Errorf("acloserlisten: album %s does not need identity resolution", rel.id)
	}
	return rel.playerURL(), nil
}

// ResolveIdentity turns a track player into the album it belongs to. A player
// with no album stays the track. The note on Metadata tells Evaluate which
// track was resolved; Evaluate removes it before publishing.
func (Flow) ResolveIdentity(page rfs.Page, item rfs.ExtractedItem) (rfs.ExtractedItem, error) {
	rel, err := itemRelease(item.GUID)
	if err != nil {
		return rfs.ExtractedItem{}, err
	}
	if rel.kind != releaseTrack {
		return rfs.ExtractedItem{}, fmt.Errorf("acloserlisten: album %s does not need identity resolution", rel.id)
	}
	meta, err := loadPlayer(page, rel.id)
	if err != nil {
		return rfs.ExtractedItem{}, err
	}
	if _, _, ok := trackFields(meta, rel.id); !ok {
		return rfs.ExtractedItem{}, fmt.Errorf("acloserlisten: player %s does not contain that track", rel.id)
	}
	releaseKey := rel.checkpointKey()
	albumID := int64(0)
	if meta.AlbumID != nil {
		albumID = *meta.AlbumID
	}
	if albumID > 0 {
		album := release{kind: releaseAlbum, id: strconv.FormatInt(albumID, 10)}
		item.GUID = album.guid()
		releaseKey = album.checkpointKey()
	}
	note, err := json.Marshal(identityNote{Track: rel.id, Release: releaseKey})
	if err != nil {
		return rfs.ExtractedItem{}, fmt.Errorf("acloserlisten: encode identity note: %w", err)
	}
	item.Metadata = string(note)
	return item, nil
}

// AnnouncementEnrichmentURL names the Bandcamp player that carries a release's
// artist and title. The URL is built from the validated ID, never from the
// page's own iframe markup.
func (Flow) AnnouncementEnrichmentURL(item rfs.ExtractedItem) (string, error) {
	rel, err := itemRelease(item.GUID)
	if err != nil {
		return "", err
	}
	return rel.playerURL(), nil
}

// EnrichAnnouncement publishes the release's own name. The player serves the
// same release the page embedded, so the title does not depend on how the
// prose happens to introduce it, and a nameless player is an error rather than
// an entry with an invented title.
func (Flow) EnrichAnnouncement(page rfs.Page, item rfs.ExtractedItem) (rfs.ExtractedItem, error) {
	rel, err := itemRelease(item.GUID)
	if err != nil {
		return rfs.ExtractedItem{}, err
	}
	meta, err := loadPlayer(page, rel.id)
	if err != nil {
		return rfs.ExtractedItem{}, err
	}
	item.Metadata = ""
	if rel.kind == releaseTrack {
		if meta.AlbumID != nil && *meta.AlbumID > 0 {
			return rfs.ExtractedItem{}, fmt.Errorf("acloserlisten: player %s belongs to album %d", rel.id, *meta.AlbumID)
		}
		trackArtist, trackTitle, ok := trackFields(meta, rel.id)
		if !ok {
			return rfs.ExtractedItem{}, fmt.Errorf("acloserlisten: player %s does not contain that track", rel.id)
		}
		artist := strings.TrimSpace(textValue(meta.Artist))
		if artist == "" {
			artist = trackArtist
		}
		if artist == "" || trackTitle == "" {
			return rfs.ExtractedItem{}, fmt.Errorf("acloserlisten: player %s has no artist and title", rel.id)
		}
		item.Title = artist + " — " + trackTitle
		return item, nil
	}
	if meta.AlbumID == nil || strconv.FormatInt(*meta.AlbumID, 10) != rel.id {
		return rfs.ExtractedItem{}, fmt.Errorf("acloserlisten: player %s serves a different album", rel.id)
	}
	artist := strings.TrimSpace(textValue(meta.Artist))
	title := strings.TrimSpace(textValue(meta.AlbumTitle))
	if artist == "" || title == "" {
		return rfs.ExtractedItem{}, fmt.Errorf("acloserlisten: player %s has no artist and title", rel.id)
	}
	item.Title = artist + " — " + title
	return item, nil
}

// bandcampPlayer is the metadata JSON a Bandcamp embedded player carries.
type bandcampPlayer struct {
	AlbumID    *int64  `json:"album_id"`
	Artist     *string `json:"artist"`
	AlbumTitle *string `json:"album_title"`
	Tracks     []struct {
		ID     *int64  `json:"id"`
		Artist *string `json:"artist"`
		Title  *string `json:"title"`
	} `json:"tracks"`
}

// loadPlayer reads player metadata. The HTML parser already decoded the
// attribute's entities, so the JSON is not unescaped a second time.
func loadPlayer(page rfs.Page, label string) (bandcampPlayer, error) {
	doc, err := rfs.ParseHTML(page)
	if err != nil {
		return bandcampPlayer{}, fmt.Errorf("acloserlisten: parse player %s: %w", label, err)
	}
	raw, ok := playerData(doc)
	if !ok {
		return bandcampPlayer{}, fmt.Errorf("acloserlisten: player %s carries no metadata", label)
	}
	var meta bandcampPlayer
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		return bandcampPlayer{}, fmt.Errorf("acloserlisten: decode player %s: %w", label, err)
	}
	return meta, nil
}

func trackFields(meta bandcampPlayer, id string) (artist, title string, ok bool) {
	for _, track := range meta.Tracks {
		if track.ID == nil || strconv.FormatInt(*track.ID, 10) != id {
			continue
		}
		return strings.TrimSpace(textValue(track.Artist)), strings.TrimSpace(textValue(track.Title)), true
	}
	return "", "", false
}

// playerData returns the player metadata JSON.
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
