package rfs

import (
	"context"
	"fmt"
)

// EnrichedChangeFlow describes optional metadata queries for emitted changes.
// rfs performs the IO; the Flow constructs queries and decodes responses.
// A nil request body selects GET; a non-nil body selects a JSON POST.
type EnrichedChangeFlow interface {
	ChangeFlow
	EnrichmentRequest([]ExtractedItem) (string, []byte, error)
	Enrich(Page, []ExtractedItem) ([]ExtractedItem, error)
}
type JSONFetcher interface {
	FetchJSON(context.Context, string, []byte) (FetchResult, error)
}

func (p Poller) enrichChanges(ctx context.Context, flow ChangeFlow, changes []ExtractedItem) ([]ExtractedItem, error) {
	enriched, ok := flow.(EnrichedChangeFlow)
	if !ok {
		return changes, nil
	}
	for start := 0; start < len(changes); start += 50 {
		end := min(start+50, len(changes))
		batch := changes[start:end]
		url, body, err := enriched.EnrichmentRequest(batch)
		if err != nil {
			return nil, err
		}
		if url == "" {
			continue
		}
		var result FetchResult
		if body == nil {
			result, err = p.Fetcher.Fetch(ctx, url, FetchCache{})
		} else {
			fetcher, ok := p.Fetcher.(JSONFetcher)
			if !ok {
				return nil, fmt.Errorf("fetcher does not support JSON metadata queries")
			}
			result, err = fetcher.FetchJSON(ctx, url, body)
		}
		if err != nil {
			return nil, err
		}
		if result.Status == FetchThrottled {
			return nil, &fetchThrottle{result.RetryAfter}
		}
		if result.Status != FetchModified {
			return nil, fmt.Errorf("metadata unavailable (status %d)", result.Status)
		}
		items, err := enriched.Enrich(result.Page, batch)
		if err != nil {
			return nil, err
		}
		if len(items) != len(batch) {
			return nil, fmt.Errorf("metadata changed item count")
		}
		copy(batch, items)
	}
	return changes, nil
}
