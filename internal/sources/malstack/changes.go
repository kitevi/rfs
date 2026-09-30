package malstack

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/kitevi/rfs/internal/rfs"
)

var canonicalID = regexp.MustCompile(`^[1-9][0-9]*$`)

type observation struct {
	id    string
	title string
	notes []string
}

// changes compares two complete observations and emits one display item per
// affected anime: added, removed, or note edited. Membership events never
// depend on notes; presentation fields (order, title, cover, scores) are
// ignored.
func changes(previous, current []rfs.ExtractedItem) ([]rfs.ExtractedItem, error) {
	before, err := decodeObservations(previous)
	if err != nil {
		return nil, err
	}
	after, err := decodeObservations(current)
	if err != nil {
		return nil, err
	}
	type event struct {
		id   string
		item rfs.ExtractedItem
	}
	var events []event
	for id, entry := range after {
		prior, ok := before[id]
		switch {
		case !ok:
			events = append(events, event{id: id, item: rfs.ExtractedItem{
				GUID:        id,
				Title:       "Added: " + entry.title,
				Link:        animeLink(id),
				Description: renderNotes("Added to the stack.", entry.notes),
			}})
		case !equalLines(prior.notes, entry.notes):
			events = append(events, event{id: id, item: rfs.ExtractedItem{
				GUID:        id,
				Title:       "Notes updated: " + entry.title,
				Link:        animeLink(id),
				Description: renderDiff(prior.notes, entry.notes),
			}})
		}
	}
	for id, entry := range before {
		if _, ok := after[id]; ok {
			continue
		}
		events = append(events, event{id: id, item: rfs.ExtractedItem{
			GUID:        id,
			Title:       "Removed: " + entry.title,
			Link:        animeLink(id),
			Description: renderNotes("Removed from the stack.", entry.notes),
		}})
	}
	sort.Slice(events, func(i, j int) bool {
		return numericID(events[i].id) < numericID(events[j].id)
	})
	result := make([]rfs.ExtractedItem, 0, len(events))
	for _, e := range events {
		result = append(result, e.item)
	}
	return result, nil
}

// decodeObservations validates stored comparison state. An undecodable
// baseline is an error: the poll must preserve the prior feed rather than
// publishing false removals.
func decodeObservations(items []rfs.ExtractedItem) (map[string]observation, error) {
	result := make(map[string]observation, len(items))
	for _, item := range items {
		if !canonicalID.MatchString(item.GUID) {
			return nil, fmt.Errorf("invalid stored observation: malformed anime identity %q", item.GUID)
		}
		if item.Title == "" {
			return nil, fmt.Errorf("invalid stored observation: missing title for anime %s", item.GUID)
		}
		if _, ok := result[item.GUID]; ok {
			return nil, fmt.Errorf("invalid stored observation: duplicate anime %s", item.GUID)
		}
		var stored comparison
		if err := json.Unmarshal([]byte(item.Description), &stored); err != nil {
			return nil, fmt.Errorf("invalid stored observation for anime %s: %v", item.GUID, err)
		}
		if stored.Version != comparisonVersion || stored.Notes == nil {
			return nil, fmt.Errorf("unsupported stored observation for anime %s", item.GUID)
		}
		result[item.GUID] = observation{id: item.GUID, title: item.Title, notes: stored.Notes}
	}
	return result, nil
}

func animeLink(id string) string {
	return "https://myanimelist.net/anime/" + id
}

// numericID orders events consistently with the feed's numeric IDs; inputs
// are canonical ([1-9][0-9]*) via decodeObservations, so Atoi cannot fail.
func numericID(id string) int {
	n, _ := strconv.Atoi(id)
	return n
}

func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// renderNotes presents a membership event with the current or last observed
// notes. Every upstream value is escaped; only fixed markup is emitted.
func renderNotes(notice string, notes []string) string {
	description := "<p>" + html.EscapeString(notice) + "</p>"
	if len(notes) == 0 {
		return description
	}
	lines := make([]string, 0, len(notes))
	for _, line := range notes {
		lines = append(lines, html.EscapeString(line))
	}
	return description + `<h3>Notes</h3><pre style="white-space:pre-wrap">` + strings.Join(lines, "\n") + "</pre>"
}

// renderDiff presents the changed span of a note edit, preserving line order
// and repetitions and trimming only common leading and trailing lines.
func renderDiff(before, after []string) string {
	for len(before) > 0 && len(after) > 0 && before[0] == after[0] {
		before = before[1:]
		after = after[1:]
	}
	for len(before) > 0 && len(after) > 0 && before[len(before)-1] == after[len(after)-1] {
		before = before[:len(before)-1]
		after = after[:len(after)-1]
	}
	var lines []string
	for _, line := range before {
		lines = append(lines, html.EscapeString("- "+line))
	}
	for _, line := range after {
		lines = append(lines, html.EscapeString("+ "+line))
	}
	if len(lines) == 0 {
		return "<p>Notes changed.</p>"
	}
	return `<p>Notes changed.</p><pre style="white-space:pre-wrap">` + strings.Join(lines, "\n") + "</pre>"
}
