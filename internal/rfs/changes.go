package rfs

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// ChangeFlow compares complete observations. Extract returns stable entity GUIDs;
// Changes returns only changed entities, with human-readable descriptions.
// Neither method performs IO. The first observation establishes a silent baseline.
type ChangeFlow interface {
	Flow
	Changes(previous, current []ExtractedItem) ([]ExtractedItem, error)
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

func (p Poller) pollChanges(ctx context.Context, source Source, flow ChangeFlow, current []ExtractedItem, cache FetchCache) (PollResult, error) {
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
			changes, err = flow.Changes(nil, current)
			if err != nil {
				return PollResult{}, err
			}
		}
	case previous.Version == flow.Version():
		changes, err = flow.Changes(previous.Items, current)
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
		items = append(items, Item{GUID: fmt.Sprintf("%s:%d:%s", source.ID, revision, change.GUID), Title: change.Title, Link: change.Link, Description: change.Description, PubDate: p.now()})
	}
	cache.ExtractVersion = flow.Version()
	err = store.SaveChanges(ctx, source.ID, ChangeState{Items: current, Version: flow.Version(), Revision: revision, Initialized: true}, items, cache)
	if err != nil {
		return PollResult{}, err
	}
	return PollResult{Status: PollUpdated}, nil
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
