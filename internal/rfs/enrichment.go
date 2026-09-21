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

func (p Poller) enrichChanges(ctx context.Context, flow Flow, changes []ExtractedItem) ([]ExtractedItem, error) {
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

// EnrichedAnnouncementFlow describes optional per-announcement metadata
// queries. Unlike EnrichedChangeFlow, whose items share one batched request,
// each announcement names its own resource, so rfs resolves them one at a
// time. rfs performs the IO; the Flow constructs the URL and decodes the
// response, and neither method performs IO of its own.
type EnrichedAnnouncementFlow interface {
	AnnouncementFlow

	// AnnouncementEnrichmentURL returns the metadata URL for one unpublished
	// announcement. An empty URL leaves that item as it is.
	AnnouncementEnrichmentURL(ExtractedItem) (string, error)

	// EnrichAnnouncement decodes one metadata response into the item to
	// publish. It must keep the item's GUID: identity, not metadata, decides
	// what has already been announced.
	EnrichAnnouncement(Page, ExtractedItem) (ExtractedItem, error)
}

// enrichAnnouncements resolves metadata for newly announced items before they
// are committed. Requests run sequentially, so a whole-page replacement cannot
// burst at the upstream host, and any failure aborts the poll before the commit:
// the checkpoint still lacks these items, so the next poll retries the batch
// instead of publishing a partial one.
func (p Poller) enrichAnnouncements(ctx context.Context, flow Flow, announcements []ExtractedItem) ([]ExtractedItem, error) {
	enriched, ok := flow.(EnrichedAnnouncementFlow)
	if !ok || len(announcements) == 0 {
		return announcements, nil
	}
	resolved := make([]ExtractedItem, len(announcements))
	for i, item := range announcements {
		url, err := enriched.AnnouncementEnrichmentURL(item)
		if err != nil {
			return nil, err
		}
		if url == "" {
			resolved[i] = item
			continue
		}
		result, err := p.Fetcher.Fetch(ctx, url, FetchCache{})
		if err != nil {
			return nil, err
		}
		if result.Status == FetchThrottled {
			return nil, &fetchThrottle{result.RetryAfter}
		}
		if result.Status != FetchModified {
			return nil, fmt.Errorf("announcement metadata unavailable (status %d)", result.Status)
		}
		item, err = enriched.EnrichAnnouncement(result.Page, item)
		if err != nil {
			return nil, err
		}
		if item.GUID != announcements[i].GUID {
			return nil, fmt.Errorf("announcement metadata changed GUID %q to %q", announcements[i].GUID, item.GUID)
		}
		resolved[i] = item
	}
	return resolved, nil
}
