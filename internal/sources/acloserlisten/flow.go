// Package acloserlisten extracts A Closer Listen's recommended releases.
//
// The recommendations page is a WordPress page the site rewrites wholesale.
// The usual revision is genre sections of Bandcamp album players. A revision
// may also embed a single track. An album player, and a track player that
// belongs to an album, are that album. A track with no album is its own
// release. A YouTube embed is that video. A bold artist followed by an italic
// title is that album, so a name written in the lede is announced even when
// the player under it is a different recording. Other players are not releases.
package acloserlisten

import (
	"encoding/json"
	"fmt"
	"net/url"

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
	// track players. Version 3 also extracts YouTube embeds. Version 4 also
	// extracts bold-artist, italic-title album names. Album-only pages still
	// produce the same items.
	ExtractVersion = 4

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

	// youtubeGUIDPrefix is the identity of an embedded YouTube video. It does
	// not share the Bandcamp album or track namespace.
	youtubeGUIDPrefix = "acloserlisten:youtube:"

	// youtubeCheckpointPrefix marks a YouTube video in the announced set.
	youtubeCheckpointPrefix = "youtube:"

	// namedGUIDPrefix is the identity of an album written as a bold artist and
	// an italic title. The slug is lowercase letters and digits, artist and
	// title separated by a colon.
	namedGUIDPrefix = "acloserlisten:named:"

	// namedCheckpointPrefix marks a prose album in the announced set.
	namedCheckpointPrefix = "named:"

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
	releaseYouTube
	releaseNamed
)

// release is one Bandcamp player identity derived from an iframe URL.
type release struct {
	kind releaseKind
	id   string
}

func (r release) guid() string {
	switch r.kind {
	case releaseTrack:
		return trackGUIDPrefix + r.id
	case releaseYouTube:
		return youtubeGUIDPrefix + r.id
	case releaseNamed:
		return namedGUIDPrefix + r.id
	default:
		return guidPrefix + r.id
	}
}

// checkpointKey is the form stored in the announced set. Album keys stay bare
// numbers so a checkpoint written before track support still matches.
func (r release) checkpointKey() string {
	switch r.kind {
	case releaseTrack:
		return trackCheckpointPrefix + r.id
	case releaseYouTube:
		return youtubeCheckpointPrefix + r.id
	case releaseNamed:
		return namedCheckpointPrefix + r.id
	default:
		return r.id
	}
}

func (r release) playerURL() string {
	switch r.kind {
	case releaseTrack:
		return "https://bandcamp.com/EmbeddedPlayer/v=2/track=" + r.id + playerURLSuffix
	case releaseYouTube:
		return "https://www.youtube.com/oembed?format=json&url=" + url.QueryEscape("https://www.youtube.com/watch?v="+r.id)
	case releaseNamed:
		return ""
	default:
		return "https://bandcamp.com/EmbeddedPlayer/v=2/album=" + r.id + playerURLSuffix
	}
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
			Title:       embedded.title,
			Link:        HumanURL,
			Description: description(embedded.genre),
		})
	}
	return items, nil
}
