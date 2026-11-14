package rfs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	// The first complete observation, and the first after an extraction-version
	// change, is a silent baseline: comparing it would announce the Flow's own
	// change, not upstream's.
	var changes []ExtractedItem
	if previous.Initialized && previous.Version == flow.Version() {
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
		item := change.item(change.pubDateOr(p.now()))
		item.GUID = fmt.Sprintf("%s:%d:%s", source.ID, revision, change.GUID)
		items = append(items, item)
	}
	cache.ExtractVersion = flow.Version()
	err = store.SaveChanges(commitContext(ctx), source.ID, ChangeState{Items: current, Version: flow.Version(), Revision: revision, Initialized: true}, items, cache)
	if err != nil {
		return PollResult{}, err
	}
	if len(items) == 0 {
		return PollResult{Status: PollUnchanged}, nil
	}
	return PollResult{Status: PollUpdated}, nil
}

func (s *SQLiteStore) LoadChangeState(ctx context.Context, sourceID string) (ChangeState, error) {
	var state ChangeState
	var data []byte
	err := s.db.QueryRowContext(ctx, `SELECT baseline, version, revision FROM change_state WHERE source_id = ?`, sourceID).Scan(&data, &state.Version, &state.Revision)
	if errors.Is(err, sql.ErrNoRows) {
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
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if err := writeItems(ctx, tx, insertItem, sourceID, items); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO change_state (source_id, baseline, version, revision) VALUES (?, ?, ?, ?) ON CONFLICT(source_id) DO UPDATE SET baseline=excluded.baseline, version=excluded.version, revision=excluded.revision`, sourceID, data, state.Version, state.Revision); err != nil {
			return err
		}
		return saveFetchCache(ctx, tx, sourceID, cache)
	})
}
