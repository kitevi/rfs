package rfs

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
)

// PaginatedFlow describes a numbered JSON collection. rfs fetches every page
// before extraction results can replace a baseline; Flows never fetch.
type PaginatedFlow interface {
	Flow
	Pagination(Page) (PageInfo, error)
}
type PageInfo struct {
	Page       int `json:"page"`
	TotalPages int `json:"totalPages"`
	TotalItems int `json:"totalItems"`
}

func (p Poller) extractPages(ctx context.Context, source Source, flow PaginatedFlow, first Page) ([]ExtractedItem, error) {
	info, err := flow.Pagination(first)
	if err != nil {
		return nil, err
	}
	if info.Page != 1 || info.TotalPages < 0 || info.TotalPages > 1000 || info.TotalItems < 0 {
		return nil, fmt.Errorf("invalid pagination for %s", source.ID)
	}
	base, err := url.Parse(source.URL)
	if err != nil {
		return nil, err
	}
	var items []ExtractedItem
	page := first
	for number := 1; number <= max(1, info.TotalPages); number++ {
		if number > 1 {
			query := base.Query()
			query.Set("page", strconv.Itoa(number))
			base.RawQuery = query.Encode()
			fetched, err := p.Fetcher.Fetch(ctx, base.String(), FetchCache{})
			if err != nil {
				return nil, err
			}
			if fetched.Status == FetchThrottled {
				return nil, &fetchThrottle{fetched.RetryAfter}
			}
			if fetched.Status != FetchModified {
				return nil, fmt.Errorf("page %d of %s unavailable (status %d)", number, source.ID, fetched.Status)
			}
			page = fetched.Page
		}
		current, err := flow.Pagination(page)
		if err != nil {
			return nil, err
		}
		if current.Page != number || current.TotalPages != info.TotalPages || current.TotalItems != info.TotalItems {
			return nil, fmt.Errorf("pagination changed during poll of %s", source.ID)
		}
		extracted, err := flow.Extract(page)
		if err != nil {
			return nil, err
		}
		items = append(items, extracted...)
	}
	if len(items) != info.TotalItems {
		return nil, fmt.Errorf("incomplete collection for %s: got %d, want %d", source.ID, len(items), info.TotalItems)
	}
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		if item.GUID == "" || seen[item.GUID] {
			return nil, fmt.Errorf("missing or duplicate identity in %s", source.ID)
		}
		seen[item.GUID] = true
	}
	return items, nil
}
