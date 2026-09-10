package rfs

import "time"

// LiveFlow reports whether a stored feed entry still describes something a
// subscriber should see. rfs asks while rendering the feed, so an entry stops
// being served as soon as its own window ends rather than when the next poll
// happens to notice. A Flow that does not implement this interface keeps its
// whole stored history, which is what the catalog feeds rely on.
type LiveFlow interface {
	Flow
	LiveAt(Item, time.Time) bool
}

// liveItems drops the stored entries a Source's Flow no longer considers live.
func liveItems(source Source, items []Item, at time.Time) []Item {
	flow, ok := source.Flow.(LiveFlow)
	if !ok {
		return items
	}
	live := make([]Item, 0, len(items))
	for _, item := range items {
		if flow.LiveAt(item, at) {
			live = append(live, item)
		}
	}
	return live
}
