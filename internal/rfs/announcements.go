package rfs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
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

// DatedAnnouncementFlow is an AnnouncementFlow whose decision depends on when
// the poll runs. The poller passes its clock; Evaluate stays available for
// Flows that do not care. Methods perform no IO.
type DatedAnnouncementFlow interface {
	AnnouncementFlow

	// EvaluateAt is Evaluate at a known instant. now is the poller's clock,
	// which is UTC unless a test fixes it.
	EvaluateAt(now time.Time, checkpoint json.RawMessage, current []ExtractedItem) (AnnouncementDecision, error)
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
	current, err = p.resolveAnnouncementIdentities(ctx, source.ID, flow, checkpoint, current)
	if err != nil {
		return pollFailure(err)
	}
	decision, err := p.decideAnnouncements(ctx, source.ID, flow, checkpoint, current)
	if err != nil {
		return pollFailure(err)
	}
	if len(decision.Checkpoint) == 0 {
		return PollResult{}, fmt.Errorf("poll %s: announcement flow returned an empty checkpoint", source.ID)
	}
	items := make([]Item, 0, len(decision.Announcements))
	for _, announcement := range decision.Announcements {
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

// decideAnnouncements derives the announcements for one poll and resolves
// their metadata before anything commits. An announcement whose metadata cannot
// be fetched is quarantined out of the observation and the decision is re-derived
// from the same checkpoint, so the checkpoint never books an item the feed did
// not publish and the next poll retries it. Each pass either stops or removes at
// least one item, so the bound is the observation size.
func (p Poller) decideAnnouncements(ctx context.Context, sourceID string, flow AnnouncementFlow, checkpoint json.RawMessage, current []ExtractedItem) (AnnouncementDecision, error) {
	at := p.now()
	resolved := make(map[string]ExtractedItem, len(current))
	for range len(current) + 1 {
		decision, err := evaluateAnnouncements(flow, at, checkpoint, current)
		if err != nil {
			return AnnouncementDecision{}, err
		}
		if len(decision.Announcements) == 0 {
			return decision, nil
		}
		published, quarantined, err := p.enrichAnnouncements(ctx, sourceID, flow, decision.Announcements, resolved)
		if err != nil {
			return AnnouncementDecision{}, err
		}
		decision.Announcements = published
		if len(quarantined) == 0 {
			return decision, nil
		}
		pruned := withoutItems(current, quarantined)
		if len(pruned) == len(current) {
			return AnnouncementDecision{}, fmt.Errorf("poll %s: a quarantined announcement is not in the observation", sourceID)
		}
		current = pruned
	}
	return AnnouncementDecision{}, fmt.Errorf("poll %s: every observed item was quarantined", sourceID)
}

// evaluateAnnouncements hands a clock-aware Flow the poll instant; the instant
// is fixed for the whole poll so a re-derived decision cannot drift.
func evaluateAnnouncements(flow AnnouncementFlow, at time.Time, checkpoint json.RawMessage, current []ExtractedItem) (AnnouncementDecision, error) {
	if dated, ok := flow.(DatedAnnouncementFlow); ok {
		return dated.EvaluateAt(at, checkpoint, current)
	}
	return flow.Evaluate(checkpoint, current)
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
