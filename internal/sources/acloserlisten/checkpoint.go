package acloserlisten

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/kitevi/rfs/internal/rfs"
)

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
	if raw, ok := strings.CutPrefix(guid, namedGUIDPrefix); ok {
		if !validNameSlug(raw) {
			return release{}, fmt.Errorf("acloserlisten: unexpected item GUID %q", guid)
		}
		return release{kind: releaseNamed, id: raw}, nil
	}
	if raw, ok := strings.CutPrefix(guid, youtubeGUIDPrefix); ok {
		if !validYouTubeID(raw) {
			return release{}, fmt.Errorf("acloserlisten: unexpected item GUID %q", guid)
		}
		return release{kind: releaseYouTube, id: raw}, nil
	}
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
	if raw, ok := strings.CutPrefix(key, namedCheckpointPrefix); ok {
		if !validNameSlug(raw) {
			return release{}, fmt.Errorf("acloserlisten: unexpected checkpoint entry %q", key)
		}
		return release{kind: releaseNamed, id: raw}, nil
	}
	if raw, ok := strings.CutPrefix(key, youtubeCheckpointPrefix); ok {
		if !validYouTubeID(raw) {
			return release{}, fmt.Errorf("acloserlisten: unexpected checkpoint entry %q", key)
		}
		return release{kind: releaseYouTube, id: raw}, nil
	}
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
