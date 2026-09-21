package rfs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// AnnouncementFlow compares a complete observation against a Flow-owned
// checkpoint to decide which items to announce. Unlike ChangeFlow, whose state
// is the previous observation, an announcement Flow remembers everything it has
// already published, so a temporarily incomplete or stale upstream list cannot
// re-announce or forget an item. Methods perform no IO, and the checkpoint
// format belongs to the Flow.
type AnnouncementFlow interface {
	Flow

	// Evaluate compares the latest observation with the stored checkpoint and
	// returns the checkpoint to persist alongside the announcements. An empty
	// checkpoint means nothing has been announced yet.
	Evaluate(checkpoint json.RawMessage, current []ExtractedItem) (AnnouncementDecision, error)
}

// AnnouncementDecision is the outcome of one announcement comparison.
type AnnouncementDecision struct {
	// Checkpoint replaces the stored checkpoint when the poll commits. It must
	// be non-empty: without durable state the next poll cannot tell which
	// announcements already happened, and would repeat them.
	Checkpoint json.RawMessage

	// Announcements are new items to publish, in display order. Their GUIDs are
	// stable, so replaying one after a failed poll cannot duplicate it.
	Announcements []ExtractedItem
}

// AnnouncementStore persists what a Flow has announced.
type AnnouncementStore interface {
	LoadAnnouncementCheckpoint(context.Context, string) (json.RawMessage, error)
	// CommitAnnouncements inserts announced items, replaces the checkpoint, and
	// advances the fetch validators in one transaction.
	CommitAnnouncements(context.Context, string, json.RawMessage, []Item, FetchCache) error
}

// pollAnnouncements runs an announcement comparison: load the checkpoint, let
// the Flow decide, then commit the decision, the announced items, and the fetch
// validators together. A failure anywhere leaves the previous feed and
// checkpoint intact so the next poll retries the same comparison.
func (p Poller) pollAnnouncements(ctx context.Context, source Source, flow AnnouncementFlow, current []ExtractedItem, cache FetchCache) (PollResult, error) {
	store, ok := p.Store.(AnnouncementStore)
	if !ok {
		return PollResult{}, fmt.Errorf("poll %s: store does not support announcements", source.ID)
	}
	checkpoint, err := store.LoadAnnouncementCheckpoint(ctx, source.ID)
	if err != nil {
		return PollResult{}, err
	}
	decision, err := flow.Evaluate(checkpoint, current)
	if err != nil {
		return pollFailure(err)
	}
	if len(decision.Checkpoint) == 0 {
		return PollResult{}, fmt.Errorf("poll %s: announcement flow returned an empty checkpoint", source.ID)
	}
	announcements, err := p.enrichAnnouncements(ctx, flow, decision.Announcements)
	if err != nil {
		return pollFailure(err)
	}
	items := make([]Item, 0, len(announcements))
	for _, announcement := range announcements {
		if announcement.GUID == "" {
			return PollResult{}, fmt.Errorf("poll %s: announcement without a GUID", source.ID)
		}
		pubDate := p.now()
		if announcement.PubDate != nil {
			pubDate = *announcement.PubDate
		}
		items = append(items, Item{
			GUID:        announcement.GUID,
			Title:       announcement.Title,
			Link:        announcement.Link,
			Description: announcement.Description,
			Metadata:    announcement.Metadata,
			PubDate:     pubDate,
		})
	}
	cache.ExtractVersion = flow.Version()
	if err := store.CommitAnnouncements(ctx, source.ID, decision.Checkpoint, items, cache); err != nil {
		return PollResult{}, err
	}
	if len(items) == 0 {
		return PollResult{Status: PollUnchanged}, nil
	}
	return PollResult{Status: PollUpdated}, nil
}

func (s *SQLiteStore) LoadAnnouncementCheckpoint(ctx context.Context, sourceID string) (json.RawMessage, error) {
	var checkpoint []byte
	err := s.db.QueryRowContext(ctx, `SELECT checkpoint FROM announcement_state WHERE source_id = ?`, sourceID).Scan(&checkpoint)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return json.RawMessage(checkpoint), nil
}

// CommitAnnouncements inserts announced items, replaces the checkpoint, and
// advances the fetch validators in one transaction. Re-inserted items are
// ignored rather than updated, so an announcement's text is immutable once
// published and a replayed decision cannot duplicate a feed entry.
func (s *SQLiteStore) CommitAnnouncements(ctx context.Context, sourceID string, checkpoint json.RawMessage, items []Item, cache FetchCache) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollbackUnlessCommitted(tx)
	for _, item := range items {
		_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO snapshots (source_id, guid, title, link, description, pub_date, replies, metadata) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, sourceID, item.GUID, item.Title, item.Link, item.Description, formatStoreTime(item.PubDate), item.Replies, item.Metadata)
		if err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO announcement_state (source_id, checkpoint) VALUES (?, ?) ON CONFLICT(source_id) DO UPDATE SET checkpoint=excluded.checkpoint`, sourceID, []byte(checkpoint))
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO fetch_cache (source_id, etag, last_modified, extract_version) VALUES (?, ?, ?, ?) ON CONFLICT(source_id) DO UPDATE SET etag=excluded.etag, last_modified=excluded.last_modified, extract_version=excluded.extract_version`, sourceID, cache.ETag, cache.LastModified, cache.ExtractVersion)
	if err != nil {
		return err
	}
	return tx.Commit()
}
