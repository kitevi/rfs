package rfs

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RebuildFromSavedInput re-derives a stored history row from the flow-owned
// extraction input saved in Item.Metadata, using the same extract function as
// fresh observation. It recomputes title, link, description, and observed
// reply counts from saved inputs, never from previously rendered text.
//
// Rows saved before inputs were captured (empty Metadata) cannot be
// reconstructed: only a leading legacy board marker is repaired, and the row
// is flagged {"legacy":true} so later rebuilds leave it untouched. T is the
// flow's input type (e.g. its catalog thread struct); extract must be pure and
// deterministic. boardMarker is the lowercase board marker (e.g. "/ptg/"), and
// stripMarker removes it from titles.
func RebuildFromSavedInput[T any](old Item, flowName, boardMarker string, extract func(T) (ExtractedItem, bool), stripMarker func(string) string) (Item, error) {
	if old.Metadata == "" {
		// Pre-input-storage rows cannot be reconstructed. Repair only the known
		// leading legacy marker; never strip a marker inside the edition text.
		if strings.HasPrefix(strings.ToLower(old.Title), boardMarker) {
			old.Title = stripMarker(old.Title)
		}
		old.Metadata = `{"legacy":true}`
		return old, nil
	}
	var marker struct {
		Legacy bool `json:"legacy"`
	}
	if err := json.Unmarshal([]byte(old.Metadata), &marker); err != nil {
		return Item{}, err
	}
	if marker.Legacy {
		return old, nil
	}
	var input T
	if err := json.Unmarshal([]byte(old.Metadata), &input); err != nil {
		return Item{}, fmt.Errorf("%s: stored input: %w", flowName, err)
	}
	item, ok := extract(input)
	if !ok || item.GUID != old.GUID {
		return Item{}, fmt.Errorf("%s: invalid stored thread or changed identity", flowName)
	}
	old.Title, old.Link, old.Description = item.Title, item.Link, item.Description
	old.Replies = item.Replies
	return old, nil
}
