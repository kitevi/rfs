// Package notices turns an operator's notice list into change-feed items.
//
// The operators publish service notices on their own sites in their own
// markup, so each operator package only has to decode a page into Notices and
// this Flow owns the shared behaviour: the notice permalink is the entity
// identity, an observed edit emits an update item with the same identity, and a
// notice that disappears emits nothing because a disappearance is not a
// cancellation.
package notices

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/ppowo/rfs/internal/rfs"
)

const (
	// ExtractVersion is bumped whenever Extract or Changes output can change for
	// a fixed page.
	ExtractVersion = 1

	payloadVersion = 1
)

// Notice is one operator notice as published upstream.
type Notice struct {
	// ID is the stable identity of the notice, normally its canonical permalink.
	ID string
	// Title is the upstream headline, without any scope label.
	Title string
	// Summary is the upstream lead text, empty when the collection has none.
	Summary string
	// Link is the notice page a reader should open.
	Link string
	// Date is the upstream date as published, empty when the collection has none.
	Date string
}

// Parser decodes one collection page. It only reads bytes rfs already fetched
// and must fail rather than return an empty list when the page no longer has
// the structure it expects.
type Parser func(rfs.Page) ([]Notice, error)

// Flow publishes the notices an operator currently lists.
type Flow struct {
	// Operator is the scope label every title carries, e.g. "Trieste Trasporti".
	Operator string
	Parser   Parser
}

func (f Flow) Version() int { return ExtractVersion }

func (f Flow) Extract(page rfs.Page) ([]rfs.ExtractedItem, error) {
	list, err := f.Parser(page)
	if err != nil {
		return nil, err
	}
	items := make([]rfs.ExtractedItem, 0, len(list))
	seen := make(map[string]bool, len(list))
	for _, notice := range list {
		if err := validate(&notice); err != nil {
			return nil, fmt.Errorf("notices: %s: %w", f.Operator, err)
		}
		if seen[notice.ID] {
			continue
		}
		seen[notice.ID] = true
		data, err := json.Marshal(payload{Version: payloadVersion, Notice: notice})
		if err != nil {
			return nil, err
		}
		items = append(items, rfs.ExtractedItem{
			GUID:        notice.ID,
			Link:        notice.Link,
			Title:       noticeTitle("", f.Operator, notice.Title),
			Description: string(data),
		})
	}
	return items, nil
}

// Changes emits one item per observed addition or edit. Notices that are
// unchanged, and notices that are no longer listed, emit nothing.
func (f Flow) Changes(previous, current []rfs.ExtractedItem) ([]rfs.ExtractedItem, error) {
	stored := make(map[string]rfs.ExtractedItem, len(previous))
	for _, item := range previous {
		stored[item.GUID] = item
	}
	var changes []rfs.ExtractedItem
	for _, item := range current {
		after, err := decodeNotice(item)
		if err != nil {
			return nil, err
		}
		before, seen := stored[item.GUID]
		delete(stored, item.GUID)
		if !seen {
			changes = append(changes, changeItem(item, noticeTitle("", f.Operator, after.Notice.Title), announcementDescription(f.Operator, after.Notice)))
			continue
		}
		beforePayload, beforeErr := decodeNotice(before)
		fields := changedFields(beforePayload.Notice, after.Notice)
		if beforeErr == nil && len(fields) == 0 {
			continue
		}
		changes = append(changes, changeItem(item, noticeTitle("Aggiornato", f.Operator, after.Notice.Title), updateDescription(f.Operator, after.Notice, fields)))
	}
	return changes, nil
}

type payload struct {
	Version int    `json:"v"`
	Notice  Notice `json:"notice"`
}

func decodeNotice(item rfs.ExtractedItem) (payload, error) {
	var decoded payload
	if err := json.Unmarshal([]byte(item.Description), &decoded); err != nil {
		return payload{}, fmt.Errorf("notices: decode notice %s: %w", item.GUID, err)
	}
	if decoded.Version != payloadVersion {
		return payload{}, fmt.Errorf("notices: notice %s has unsupported payload version %d", item.GUID, decoded.Version)
	}
	return decoded, nil
}

func validate(notice *Notice) error {
	if strings.TrimSpace(notice.Title) == "" {
		return errors.New("notice has no title")
	}
	for _, candidate := range []string{notice.ID, notice.Link} {
		parsed, err := url.Parse(candidate)
		if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
			return fmt.Errorf("notice %q has no usable permalink", notice.Title)
		}
	}
	return nil
}

func changeItem(source rfs.ExtractedItem, title, description string) rfs.ExtractedItem {
	return rfs.ExtractedItem{GUID: source.GUID, Link: source.Link, Title: title, Description: description}
}

func noticeTitle(prefix, operator, title string) string {
	tag := "Bus"
	if prefix != "" {
		tag = prefix + " · Bus"
	}
	return "[" + tag + " · " + operator + "] " + title
}

func announcementDescription(operator string, notice Notice) string {
	var builder strings.Builder
	writeField(&builder, "Operatore", operator)
	writeField(&builder, "Data", notice.Date)
	writeField(&builder, "Titolo", notice.Title)
	writeField(&builder, "Pagina", notice.Link)
	if notice.Summary != "" {
		builder.WriteString("\n" + notice.Summary + "\n")
	}
	return strings.TrimSpace(builder.String())
}

type fieldChange struct {
	label  string
	before string
	after  string
}

func changedFields(before, after Notice) []fieldChange {
	fields := []struct {
		label string
		get   func(Notice) string
	}{
		{"Titolo", func(n Notice) string { return n.Title }},
		{"Riepilogo", func(n Notice) string { return n.Summary }},
		{"Data", func(n Notice) string { return n.Date }},
		{"Pagina", func(n Notice) string { return n.Link }},
	}
	var changes []fieldChange
	for _, field := range fields {
		oldValue, newValue := field.get(before), field.get(after)
		if oldValue == newValue {
			continue
		}
		changes = append(changes, fieldChange{label: field.label, before: oldValue, after: newValue})
	}
	return changes
}

func updateDescription(operator string, notice Notice, fields []fieldChange) string {
	var builder strings.Builder
	builder.WriteString("Aggiornamento dell'avviso: " + notice.Title)
	if notice.Date != "" {
		builder.WriteString(" (" + notice.Date + ")")
	}
	builder.WriteString("\n" + operator + "\n")
	for _, field := range fields {
		builder.WriteString("\n" + field.label + ": " + displayValue(field.before) + " → " + displayValue(field.after))
	}
	return strings.TrimRight(builder.String(), "\n")
}

func displayValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func writeField(builder *strings.Builder, label, value string) {
	if strings.TrimSpace(value) == "" {
		return
	}
	builder.WriteString(label + ": " + value + "\n")
}
