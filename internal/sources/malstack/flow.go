package malstack

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/kitevi/rfs/internal/rfs"
)

const (
	PageURL  = "https://myanimelist.net/stacks/82158"
	HumanURL = PageURL
	StackID  = "82158"
	// ExtractVersion is bumped whenever Extract's output can change for a
	// fixed page.
	ExtractVersion = 1
)

var animePath = regexp.MustCompile(`^/anime/([1-9][0-9]*)/`)

type Flow struct{}

func (Flow) Version() int { return ExtractVersion }

// Extract returns one item per anime currently in the stack. Item GUIDs are
// MAL anime IDs; comparison state lives in the opaque description.
func (Flow) Extract(page rfs.Page) ([]rfs.ExtractedItem, error) {
	doc, err := rfs.ParseHTML(page)
	if err != nil {
		return nil, err
	}
	stack, err := parseStack(doc)
	if err != nil {
		return nil, err
	}
	items := make([]rfs.ExtractedItem, 0, len(stack.anime))
	for _, entry := range stack.anime {
		description, err := encodeComparison(entry.notes)
		if err != nil {
			return nil, err
		}
		items = append(items, rfs.ExtractedItem{
			GUID:        entry.id,
			Title:       entry.title,
			Link:        animeLink(entry.id),
			Description: description,
		})
	}
	return items, nil
}

func (Flow) Changes(previous, current []rfs.ExtractedItem) ([]rfs.ExtractedItem, error) {
	return changes(previous, current)
}

var _ rfs.ChangeFlow = Flow{}

func encodeComparison(notes []string) (string, error) {
	if notes == nil {
		notes = []string{}
	}
	data, err := json.Marshal(comparison{Version: comparisonVersion, Notes: notes})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func animeID(href string) (string, bool) {
	href = strings.TrimSpace(href)
	match := animePath.FindStringSubmatch(href)
	if len(match) == 2 {
		return match[1], true
	}
	for _, prefix := range []string{"https://myanimelist.net", "http://myanimelist.net"} {
		if strings.HasPrefix(href, prefix) {
			match = animePath.FindStringSubmatch(strings.TrimPrefix(href, prefix))
			if len(match) == 2 {
				return match[1], true
			}
		}
	}
	return "", false
}
