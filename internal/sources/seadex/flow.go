package seadex

import (
	"encoding/json"
	"fmt"
	"html"
	"sort"
	"strings"

	"github.com/kitevi/rfs/internal/rfs"
)

const (
	HumanURL = "https://releases.moe/"
	// Fetch the entire index in stable ID order, omitting large torrent file lists.
	PageURL        = "https://releases.moe/api/collections/entries/records?perPage=500&sort=id&expand=trs&fields=id,alID,notes,theoreticalBest,incomplete,comparison,trs,expand.trs.id,expand.trs.releaseGroup,expand.trs.isBest,expand.trs.tags,expand.trs.dualAudio,expand.trs.url,expand.trs.tracker"
	ExtractVersion = 2
)

// CachedMetadataURL selects SeaDex-hosted titles and covers. MetadataURL is
// an optional direct GraphQL alternative. Both empty leaves AniList-ID titles.
type Flow struct {
	MetadataURL       string
	CachedMetadataURL string
}

func (Flow) Version() int { return ExtractVersion }

func (Flow) Pagination(page rfs.Page) (rfs.PageInfo, error) {
	var response struct {
		Page       *int `json:"page"`
		TotalPages *int `json:"totalPages"`
		TotalItems *int `json:"totalItems"`
	}
	if err := json.Unmarshal(page, &response); err != nil {
		return rfs.PageInfo{}, err
	}
	if response.Page == nil || response.TotalPages == nil || response.TotalItems == nil {
		return rfs.PageInfo{}, fmt.Errorf("missing SeaDex pagination")
	}
	return rfs.PageInfo{Page: *response.Page, TotalPages: *response.TotalPages, TotalItems: *response.TotalItems}, nil
}

type entry struct {
	TorrentIDs      []string `json:"trs"`
	ID              string   `json:"id"`
	AlID            int      `json:"alID"`
	Notes           string   `json:"notes"`
	TheoreticalBest string   `json:"theoreticalBest"`
	Incomplete      bool     `json:"incomplete"`
	Comparison      string   `json:"comparison"`
	Expand          struct {
		Torrents []torrent `json:"trs"`
	} `json:"expand"`
}
type torrent struct {
	URL       string   `json:"url"`
	Tracker   string   `json:"tracker"`
	ID        string   `json:"id"`
	Group     string   `json:"releaseGroup"`
	Best      *bool    `json:"isBest"`
	Tags      []string `json:"tags"`
	DualAudio bool     `json:"dualAudio"`
}
type recommendation struct {
	Releases    []string
	Best        []string
	Alt         []string
	Notes       []string
	UnmuxedBest []string
	Tags        []string
	DualAudio   []string
	Incomplete  []string
	Comparisons []string
}

func (Flow) Extract(page rfs.Page) ([]rfs.ExtractedItem, error) {
	var response struct {
		Items []entry `json:"items"`
	}
	if err := json.Unmarshal(page, &response); err != nil {
		return nil, err
	}
	if response.Items == nil {
		return nil, fmt.Errorf("missing SeaDex items")
	}
	items := make([]rfs.ExtractedItem, 0, len(response.Items))
	for _, e := range response.Items {
		if e.ID == "" || e.AlID <= 0 || e.TorrentIDs == nil {
			return nil, fmt.Errorf("invalid SeaDex identity or torrent relations")
		}
		expanded := make(map[string]bool)
		for _, tr := range e.Expand.Torrents {
			if tr.ID == "" || tr.Group == "" || tr.Best == nil || expanded[tr.ID] {
				return nil, fmt.Errorf("invalid SeaDex torrent expansion")
			}
			expanded[tr.ID] = true
		}
		if len(expanded) != len(e.TorrentIDs) {
			return nil, fmt.Errorf("incomplete SeaDex torrent expansion")
		}
		for _, id := range e.TorrentIDs {
			if !expanded[id] {
				return nil, fmt.Errorf("missing expanded torrent %s", id)
			}
		}
		state := recommendation{Notes: textLines(e.Notes), UnmuxedBest: textLines(e.TheoreticalBest), Comparisons: unique(strings.Split(e.Comparison, ","))}
		if e.Incomplete {
			state.Incomplete = []string{"Yes"}
		}
		for _, tr := range e.Expand.Torrents {
			role := "Alt"
			if *tr.Best {
				role = "Best"
			}
			state.Releases = append(state.Releases, fmt.Sprintf("%s: %s [%s] %s %s", role, tr.Group, tr.ID, tr.Tracker, tr.URL))
			for _, tag := range tr.Tags {
				state.Tags = append(state.Tags, tr.Group+": "+tag)
			}
			if tr.DualAudio {
				state.DualAudio = append(state.DualAudio, tr.Group)
			}
			if *tr.Best {
				state.Best = append(state.Best, tr.Group)
			} else {
				state.Alt = append(state.Alt, tr.Group)
			}
		}
		state.Releases = unique(state.Releases)
		state.Tags = unique(state.Tags)
		state.DualAudio = unique(state.DualAudio)
		state.Best = unique(state.Best)
		state.Alt = unique(state.Alt)
		data, err := json.Marshal(state)
		if err != nil {
			return nil, err
		}
		items = append(items, rfs.ExtractedItem{GUID: fmt.Sprint(e.AlID), Title: fmt.Sprintf("AniList %d", e.AlID), Link: fmt.Sprintf("https://releases.moe/%d/", e.AlID), Description: string(data)})
	}
	return items, nil
}

func (Flow) Changes(previous, current []rfs.ExtractedItem) ([]rfs.ExtractedItem, error) {
	old := make(map[string]rfs.ExtractedItem, len(previous))
	for _, item := range previous {
		old[item.GUID] = item
	}
	var changes []rfs.ExtractedItem
	emit := func(before, after rfs.ExtractedItem) error {
		var a, b recommendation
		if before.Description != "" {
			if err := json.Unmarshal([]byte(before.Description), &a); err != nil {
				return err
			}
		}
		if after.Description != "" {
			if err := json.Unmarshal([]byte(after.Description), &b); err != nil {
				return err
			}
		}
		var sections []string
		for _, field := range []struct {
			name     string
			old, new []string
		}{{"Best", a.Best, b.Best}, {"Alt", a.Alt, b.Alt}, {"Unmuxed Best", a.UnmuxedBest, b.UnmuxedBest}, {"Notes", a.Notes, b.Notes}, {"Tags", a.Tags, b.Tags}, {"Dual Audio", a.DualAudio, b.DualAudio}, {"Incomplete", a.Incomplete, b.Incomplete}, {"Comparisons", a.Comparisons, b.Comparisons}, {"Releases", a.Releases, b.Releases}} {
			lines := diff(field.old, field.new)
			if len(lines) > 0 {
				var rendered []string
				for _, line := range lines {
					color := "#b42318"
					if strings.HasPrefix(line, "+ ") {
						color = "#067647"
					}
					rendered = append(rendered, `<span style="color:`+color+`">`+html.EscapeString(line)+`</span>`)
				}
				sections = append(sections, "<h3>"+field.name+`</h3><pre style="white-space:pre-wrap">`+strings.Join(rendered, "\n")+"</pre>")
			}
		}
		if len(sections) == 0 {
			return nil
		}
		item := after
		if item.GUID == "" {
			item = before
		}
		item.Description = strings.Join(sections, "\n\n")
		changes = append(changes, item)
		return nil
	}
	for _, item := range current {
		if err := emit(old[item.GUID], item); err != nil {
			return nil, err
		}
		delete(old, item.GUID)
	}
	var removed []string
	for id := range old {
		removed = append(removed, id)
	}
	sort.Strings(removed)
	for _, id := range removed {
		if err := emit(old[id], rfs.ExtractedItem{}); err != nil {
			return nil, err
		}
	}
	return changes, nil
}

// Diff the changed span, preserving line order and repeated lines. Trimming
// only common edges also bounds work for unusually long upstream notes.
func diff(before, after []string) []string {
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
		lines = append(lines, "- "+line)
	}
	for _, line := range after {
		lines = append(lines, "+ "+line)
	}
	return lines
}
func textLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
}

func unique(values []string) []string {
	set := make(map[string]bool)
	for _, value := range values {
		if value != "" {
			set[value] = true
		}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
