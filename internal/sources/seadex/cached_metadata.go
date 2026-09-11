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

func cachedRequest(endpoint string, items []rfs.ExtractedItem) (string, []byte, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", nil, err
	}
	filters := make([]string, 0, len(items))
	for _, item := range items {
		id, err := strconv.Atoi(item.GUID)
		if err != nil || id <= 0 {
			return "", nil, fmt.Errorf("invalid AniList ID %q", item.GUID)
		}
		filters = append(filters, "alID="+strconv.Itoa(id))
	}
	q := u.Query()
	q.Set("filter", strings.Join(filters, " || "))
	q.Set("perPage", "50")
	q.Set("fields", "alID,title_english,title_userPreferred,coverImage_medium")
	u.RawQuery = q.Encode()
	return u.String(), nil, nil
}

func enrichCached(page rfs.Page, items []rfs.ExtractedItem) ([]rfs.ExtractedItem, error) {
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
