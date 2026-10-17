package rfs

import (
	"context"
	"errors"
)

// quarantinable reports whether one item's failure may be skipped for this poll
// instead of failing the whole source. Throttling and cancellation are
// source-wide: they must reach the poll gate or stop the poll, so they stay
// fatal. Everything else describes one unreachable or unparseable item, which
// is dropped from this observation and retried on the next poll rather than
// blocking every other item the source carries.
func quarantinable(err error) bool {
	var throttle *fetchThrottle
	if errors.As(err, &throttle) {
		return false
	}
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

// withoutItems returns items minus those sharing a GUID with drop.
func withoutItems(items []ExtractedItem, drop []ExtractedItem) []ExtractedItem {
	if len(drop) == 0 {
		return items
	}
	dropGUIDs := make(map[string]bool, len(drop))
	for _, item := range drop {
		dropGUIDs[item.GUID] = true
	}
	kept := make([]ExtractedItem, 0, len(items))
	for _, item := range items {
		if dropGUIDs[item.GUID] {
			continue
		}
		kept = append(kept, item)
	}
	return kept
}
