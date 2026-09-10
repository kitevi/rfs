package rfs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// ChangeFlow compares complete observations. Extract returns stable entity GUIDs;
// Changes returns only changed entities, with human-readable descriptions.
// Neither method performs IO. The first observation establishes a silent baseline.
type ChangeFlow interface {
	Flow
	Changes(previous, current []ExtractedItem) ([]ExtractedItem, error)
}

// ClockedChangeFlow is a ChangeFlow whose comparison depends on the poll
// instant. rfs supplies the instant from its own clock, so a Flow never reads
// the ambient time and the comparison stays testable. It is a separate contract
// rather than an addition to ChangeFlow: a Flow whose decision needs the instant
// should not also have to offer a comparison that has none. A ChangeFlow that
// does not implement this interface keeps comparing without an instant.
type ClockedChangeFlow interface {
	Flow
	ChangesAt(time.Time, []ExtractedItem, []ExtractedItem) ([]ExtractedItem, error)
}

type ChangeState struct {
	Items       []ExtractedItem
	Version     int
	Revision    int64
	Initialized bool
}

type ChangeStore interface {
	LoadChangeState(context.Context, string) (ChangeState, error)
	SaveChanges(context.Context, string, ChangeState, []Item, FetchCache) error
}

// comparesChanges reports whether a Flow's entries are derived by comparison
// instead of being projected from the page every poll.
func comparesChanges(flow Flow) bool {
	switch flow.(type) {
	case ChangeFlow, ClockedChangeFlow:
		return true
	}
	return false
}

func (p Poller) pollChanges(ctx context.Context, source Source, flow Flow, current []ExtractedItem, cache FetchCache) (PollResult, error) {
	store, ok := p.Store.(ChangeStore)
	if !ok {
		return PollResult{}, fmt.Errorf("poll %s: store does not support changes", source.ID)
	}
	previous, err := store.LoadChangeState(ctx, source.ID)
	if err != nil {
		return PollResult{}, err
	}
	var changes []ExtractedItem
	switch {
	case !previous.Initialized:
		// The first complete observation is the feed's starting point. Sources
		// that opt in publish it; everything else starts from silence.
		if source.EmitInitial {
			changes, err = changesAt(flow, p.now(), nil, current)
			if err != nil {
				return PollResult{}, err
			}
		}
	case previous.Version == flow.Version() || source.EmitVersionChanges:
		// A matching version compares as usual. A Source that opted in also
		// compares across an extraction-version change, so a widened Flow can
		// publish its newly covered observations instead of dropping them
		// behind a silent rebaseline. The Flow has to treat an older payload as
		// comparison state; a bump that changes every payload announces the
		// whole feed again, which is why this stays opt-in.
		changes, err = changesAt(flow, p.now(), previous.Items, current)
		if err != nil {
			return PollResult{}, err
		}
	}
	changes, err = p.enrichChanges(ctx, flow, changes)
	if err != nil {
		return pollFailure(err)
	}
	revision := previous.Revision
	if len(changes) > 0 {
		revision++
	}
	items := make([]Item, 0, len(changes))
	for _, change := range changes {
		items = append(items, Item{GUID: fmt.Sprintf("%s:%d:%s", source.ID, revision, change.GUID), Title: change.Title, Link: change.Link, Description: change.Description, PubDate: changeTime(change, p.now())})
	}
	cache.ExtractVersion = flow.Version()
	err = store.SaveChanges(ctx, source.ID, ChangeState{Items: current, Version: flow.Version(), Revision: revision, Initialized: true}, items, cache)
	if err != nil {
		return PollResult{}, err
	}
	return PollResult{Status: PollUpdated}, nil
}

// changesAt runs the comparison, handing a clock-aware Flow the poll instant so
// its decision cannot drift with the wall clock.
func changesAt(flow Flow, at time.Time, previous, current []ExtractedItem) ([]ExtractedItem, error) {
	switch typed := flow.(type) {
	case ClockedChangeFlow:
		return typed.ChangesAt(at, previous, current)
	case ChangeFlow:
		return typed.Changes(previous, current)
	}
	return nil, fmt.Errorf("flow does not compare observations")
}

// changeTime picks the emitted timestamp: the Flow's own publication time when
// it reported one, otherwise the observation time the poller would use anyway.
func changeTime(change ExtractedItem, observed time.Time) time.Time {
	if change.PubDate != nil {
		return *change.PubDate
	}
	return observed
}

func (s *SQLiteStore) LoadChangeState(ctx context.Context, sourceID string) (ChangeState, error) {
	var state ChangeState
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT baseline, version, revision FROM change_state WHERE source_id = ?`, sourceID).Scan(&data, &state.Version, &state.Revision)
	if err == sql.ErrNoRows {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state.Items); err != nil {
		return state, err
	}
	state.Initialized = true
	return state, nil
}

// SaveChanges commits the baseline, emitted items and fetch validators together.
// A failed poll cannot consume a change without publishing it.
func (s *SQLiteStore) SaveChanges(ctx context.Context, sourceID string, state ChangeState, items []Item, cache FetchCache) error {
	data, err := json.Marshal(state.Items)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollbackUnlessCommitted(tx)
	for _, item := range items {
		_, err = tx.ExecContext(ctx, `INSERT INTO snapshots (source_id, guid, title, link, description, pub_date) VALUES (?, ?, ?, ?, ?, ?)`, sourceID, item.GUID, item.Title, item.Link, item.Description, formatStoreTime(item.PubDate))
		if err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO change_state (source_id, baseline, version, revision) VALUES (?, ?, ?, ?) ON CONFLICT(source_id) DO UPDATE SET baseline=excluded.baseline, version=excluded.version, revision=excluded.revision`, sourceID, data, state.Version, state.Revision)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO fetch_cache (source_id, etag, last_modified, extract_version) VALUES (?, ?, ?, ?) ON CONFLICT(source_id) DO UPDATE SET etag=excluded.etag, last_modified=excluded.last_modified, extract_version=excluded.extract_version`, sourceID, cache.ETag, cache.LastModified, cache.ExtractVersion)
	if err != nil {
		return err
	}
	return tx.Commit()
}
