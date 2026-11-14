package seadex

import (
	"encoding/json"
	"fmt"
	"html"
	"net/url"
	"strconv"
	"strings"

	"github.com/kitevi/rfs/internal/rfs"
)

// CachedMetadataURL is SeaDex's own copy of the AniList records it lists.
const CachedMetadataURL = "https://releases.moe/api/collections/anilist/records"

// EnrichmentURL asks SeaDex's hosted AniList collection for the changed anime.
// A Flow without a metadata URL publishes AniList-ID titles.
func (f Flow) EnrichmentURL(items []rfs.ExtractedItem) (string, error) {
	if f.CachedMetadataURL == "" {
		return "", nil
	}
	u, err := url.Parse(f.CachedMetadataURL)
	if err != nil {
		return "", err
	}
	filters := make([]string, 0, len(items))
	for _, item := range items {
		id, err := strconv.Atoi(item.GUID)
		if err != nil || id <= 0 {
			return "", fmt.Errorf("invalid AniList ID %q", item.GUID)
		}
		filters = append(filters, "alID="+strconv.Itoa(id))
	}
	q := u.Query()
	q.Set("filter", strings.Join(filters, " || "))
	q.Set("perPage", "50")
	q.Set("fields", "alID,title_english,title_userPreferred,coverImage_medium")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Enrich titles each changed anime and prepends its cover. A missing title
// keeps the AniList ID, and a cover that is not an HTTP(S) URL is skipped.
func (Flow) Enrich(page rfs.Page, items []rfs.ExtractedItem) ([]rfs.ExtractedItem, error) {
	var response struct {
		Items []struct {
			ID        int    `json:"alID"`
			English   string `json:"title_english"`
			Preferred string `json:"title_userPreferred"`
			Cover     string `json:"coverImage_medium"`
		} `json:"items"`
	}
	if err := json.Unmarshal(page, &response); err != nil {
		return nil, err
	}
	if response.Items == nil {
		return nil, fmt.Errorf("missing SeaDex metadata items")
	}
	for i := range items {
		for _, m := range response.Items {
			if strconv.Itoa(m.ID) != items[i].GUID {
				continue
			}
			title := m.English
			if title == "" {
				title = m.Preferred
			}
			if title != "" {
				items[i].Title = title
			}
			cover, err := url.Parse(m.Cover)
			if err == nil && (cover.Scheme == "https" || cover.Scheme == "http") && cover.Host != "" {
				items[i].Description = `<img src="` + html.EscapeString(cover.String()) + `" alt="` + html.EscapeString(items[i].Title) + `" width="100">` + items[i].Description
			}
			break
		}
	}
	return items, nil
}
