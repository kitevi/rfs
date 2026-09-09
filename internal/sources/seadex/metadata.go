package seadex

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"strconv"
	"strings"

	"github.com/ppowo/rfs/internal/rfs"
)

const AniListURL = "https://graphql.anilist.co"
const CachedMetadataURL = "https://releases.moe/api/collections/anilist/records"

func (f Flow) EnrichmentRequest(items []rfs.ExtractedItem) (string, []byte, error) {
	if f.CachedMetadataURL != "" {
		return cachedRequest(f.CachedMetadataURL, items)
	}
	if f.MetadataURL == "" {
		return "", nil, nil
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		id, err := strconv.Atoi(item.GUID)
		if err != nil || id <= 0 {
			return "", nil, fmt.Errorf("invalid AniList ID %q", item.GUID)
		}
		ids = append(ids, strconv.Itoa(id))
	}
	query := fmt.Sprintf(`{Page(page:1,perPage:50){media(id_in:[%s],type:ANIME){id title{english romaji} coverImage{large}}}}`, strings.Join(ids, ","))
	body, err := json.Marshal(struct {
		Query string `json:"query"`
	}{query})
	return f.MetadataURL, body, err
}

func (f Flow) Enrich(page rfs.Page, items []rfs.ExtractedItem) ([]rfs.ExtractedItem, error) {
	if f.CachedMetadataURL != "" {
		return enrichCached(page, items)
	}
	type media struct {
		ID    int `json:"id"`
		Title struct {
			English string `json:"english"`
			Romaji  string `json:"romaji"`
		} `json:"title"`
		Cover struct {
			Large string `json:"large"`
		} `json:"coverImage"`
	}
	var response struct {
		Errors []struct {
			Path []any `json:"path"`
		} `json:"errors"`
		Data *struct {
			Page *struct {
				Media []media `json:"media"`
			} `json:"Page"`
		} `json:"data"`
	}
	if err := json.Unmarshal(page, &response); err != nil {
		return nil, err
	}
	for _, failure := range response.Errors {
		path := failure.Path
		if len(path) < 4 || path[0] != "Page" || path[1] != "media" || path[3] != "coverImage" {
			return nil, fmt.Errorf("AniList metadata query failed")
		}
	}
	if response.Data == nil || response.Data.Page == nil {
		return nil, fmt.Errorf("AniList metadata query failed")
	}
	byID := make(map[string]media)
	for _, m := range response.Data.Page.Media {
		byID[strconv.Itoa(m.ID)] = m
	}
	for i := range items {
		m, ok := byID[items[i].GUID]
		if !ok {
			continue
		}
		title := m.Title.English
		if title == "" {
			title = m.Title.Romaji
		}
		if title != "" {
			items[i].Title = title
		}
		cover, err := url.Parse(m.Cover.Large)
		if err == nil && (cover.Scheme == "https" || cover.Scheme == "http") && cover.Host != "" {
			items[i].Description = `<img src="` + html.EscapeString(cover.String()) + `" alt="` + html.EscapeString(items[i].Title) + `" width="100">` + items[i].Description
		}
	}
	return items, nil
}
