package acloserlisten

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/net/html"

	"github.com/kitevi/rfs/internal/rfs"
)

// PlanIdentities rewrites track players whose release was already resolved and
// reports the rest, along with album players whose name is not yet known. Each
// album player is read once, so a later poll of the same page fetches nothing.
func (Flow) PlanIdentities(raw json.RawMessage, current []rfs.ExtractedItem) ([]rfs.ExtractedItem, []rfs.ExtractedItem, error) {
	known := map[string]string{}
	named := map[string]bool{}
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
		for key := range state.Names {
			named[key] = true
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
		if rel.kind == releaseAlbum && !named[rel.checkpointKey()] {
			unresolved = true
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

// IdentityURL names the player that can say whether a track embed is an album
// or a standalone track, and what an album is called.
func (Flow) IdentityURL(item rfs.ExtractedItem) (string, error) {
	rel, err := itemRelease(item.GUID)
	if err != nil {
		return "", err
	}
	if rel.kind != releaseTrack && rel.kind != releaseAlbum {
		return "", fmt.Errorf("acloserlisten: release %s does not need identity resolution", rel.id)
	}
	return rel.playerURL(), nil
}

// ResolveIdentity turns a track player into the album it belongs to. A player
// with no album stays the track. An album, embedded or reached through a track,
// is titled from its player and noted with the names the lede could give it.
// The note on Metadata tells Evaluate what was resolved; Evaluate removes it
// before publishing.
func (Flow) ResolveIdentity(page rfs.Page, item rfs.ExtractedItem) (rfs.ExtractedItem, error) {
	rel, err := itemRelease(item.GUID)
	if err != nil {
		return rfs.ExtractedItem{}, err
	}
	if rel.kind != releaseTrack && rel.kind != releaseAlbum {
		return rfs.ExtractedItem{}, fmt.Errorf("acloserlisten: release %s does not need identity resolution", rel.id)
	}
	meta, err := loadPlayer(page, rel.id)
	if err != nil {
		return rfs.ExtractedItem{}, err
	}
	if rel.kind == releaseAlbum {
		if meta.AlbumID == nil || strconv.FormatInt(*meta.AlbumID, 10) != rel.id {
			return rfs.ExtractedItem{}, fmt.Errorf("acloserlisten: player %s serves a different album", rel.id)
		}
		return noteAlbum(item, meta, identityNote{Release: rel.checkpointKey()})
	}
	if _, _, ok := trackFields(meta, rel.id); !ok {
		return rfs.ExtractedItem{}, fmt.Errorf("acloserlisten: player %s does not contain that track", rel.id)
	}
	if meta.AlbumID != nil && *meta.AlbumID > 0 {
		album := release{kind: releaseAlbum, id: strconv.FormatInt(*meta.AlbumID, 10)}
		item.GUID = album.guid()
		return noteAlbum(item, meta, identityNote{Track: rel.id, Release: album.checkpointKey()})
	}
	return withNote(item, identityNote{Track: rel.id, Release: rel.checkpointKey()})
}

// noteAlbum titles an album item from its player and notes its prose names. A
// player without a usable artist and title leaves the title to
// EnrichAnnouncement, which rejects it, and notes no names.
func noteAlbum(item rfs.ExtractedItem, meta bandcampPlayer, note identityNote) (rfs.ExtractedItem, error) {
	artist := strings.TrimSpace(textValue(meta.Artist))
	title := strings.TrimSpace(textValue(meta.AlbumTitle))
	note.Names = []string{}
	if artist != "" && title != "" {
		item.Title = artist + " — " + title
		note.Names = albumNames(artist, title)
	}
	return withNote(item, note)
}

func withNote(item rfs.ExtractedItem, note identityNote) (rfs.ExtractedItem, error) {
	encoded, err := json.Marshal(note)
	if err != nil {
		return rfs.ExtractedItem{}, fmt.Errorf("acloserlisten: encode identity note: %w", err)
	}
	item.Metadata = string(encoded)
	return item, nil
}

// AnnouncementEnrichmentURL names the Bandcamp player that carries a release's
// artist and title. The URL is built from the validated ID, never from the
// page's own iframe markup. An album ResolveIdentity already titled in this
// poll needs no second fetch.
func (Flow) AnnouncementEnrichmentURL(item rfs.ExtractedItem) (string, error) {
	rel, err := itemRelease(item.GUID)
	if err != nil {
		return "", err
	}
	if rel.kind == releaseAlbum && item.Title != "" {
		return "", nil
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
	if rel.kind == releaseYouTube {
		return enrichYouTube(page, item, rel.id)
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

// enrichYouTube titles a video from its oEmbed response. A title that already
// begins with the channel's artist keeps the remainder, so "Max Richter - Now
// We Can Sing" becomes "Max Richter — Now We Can Sing". The artist is matched
// by letters and digits after dropping a VEVO or " - Topic" channel suffix,
// and the title's own spelling of it is kept: channel MaxRichterVEVO still
// publishes "Max Richter — Now We Can Sing".
func enrichYouTube(page rfs.Page, item rfs.ExtractedItem, id string) (rfs.ExtractedItem, error) {
	var meta struct {
		Title  *string `json:"title"`
		Author *string `json:"author_name"`
	}
	if err := json.Unmarshal(page, &meta); err != nil {
		return rfs.ExtractedItem{}, fmt.Errorf("acloserlisten: decode video %s: %w", id, err)
	}
	title := strings.TrimSpace(textValue(meta.Title))
	author := strings.TrimSpace(textValue(meta.Author))
	if title == "" || author == "" {
		return rfs.ExtractedItem{}, fmt.Errorf("acloserlisten: video %s has no artist and title", id)
	}
	artist := strings.TrimSuffix(strings.TrimSuffix(author, " - Topic"), "VEVO")
	for _, sep := range []string{" — ", " – ", " - "} {
		lead, rest, ok := strings.Cut(title, sep)
		if ok && nameSlug(lead) != "" && nameSlug(lead) == nameSlug(artist) {
			author, title = strings.TrimSpace(lead), strings.TrimSpace(rest)
			break
		}
	}
	if title == "" {
		return rfs.ExtractedItem{}, fmt.Errorf("acloserlisten: video %s has no artist and title", id)
	}
	item.Title = author + " — " + title
	item.Metadata = ""
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
