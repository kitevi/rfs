package rfs

import "context"

// MergeHistory seeds history for tests: a commit with no rebuild that leaves
// the validators at their zero value.
func (s *SQLiteStore) MergeHistory(ctx context.Context, sourceID string, items []Item, liveGUIDs []string, keepStored int) error {
	return s.CommitHistory(ctx, sourceID, items, liveGUIDs, keepStored, FetchCache{}, nil)
}
