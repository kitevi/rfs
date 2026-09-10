package rfs

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// DetailFlow describes a collection whose notices cannot be identified or
// classified from the collection page alone. rfs, not the Flow, fetches the
// declared detail pages through the shared fetcher before extraction, so a
// Flow never performs IO.
//
// Detail URLs must be unique and stay on the Source URL's HTTPS origin. A
// collection that cannot be assembled completely fails the poll before any
// baseline or feed item is committed, so a partial observation never replaces
// a complete one. Because an active detail body can change while the
// collection page does not, detail sources never send conditional validators
// and a 304 answer is an error rather than "unchanged".
type DetailFlow interface {
	Flow

	// DetailURLs returns the detail pages required for this collection, in the
	// order ExtractDetails receives them. Relative URLs resolve against the
	// Source URL.
	DetailURLs(Page) ([]string, error)

	// ExtractDetails extracts items from the collection page and the pages
	// fetched for it, in the order DetailURLs declared them. Like Extract, it
	// only decodes bytes rfs already fetched.
	ExtractDetails(Page, []Page) ([]ExtractedItem, error)
}

// maxDetailPages bounds one poll's secondary requests. A collection larger
// than this is a Flow or upstream defect, not a bigger poll.
const maxDetailPages = 100

// extractObservation derives items for a Source, fetching any secondary pages
// its Flow declares before extraction.
func (p Poller) extractObservation(ctx context.Context, source Source, first Page) ([]ExtractedItem, error) {
	if flow, ok := source.Flow.(DetailFlow); ok {
		return p.extractDetails(ctx, source, flow, first)
	}
	if flow, ok := source.Flow.(PaginatedFlow); ok {
		return p.extractPages(ctx, source, flow, first)
	}
	return source.Flow.Extract(first)
}

func (p Poller) extractDetails(ctx context.Context, source Source, flow DetailFlow, collection Page) ([]ExtractedItem, error) {
	declared, err := flow.DetailURLs(collection)
	if err != nil {
		return nil, err
	}
	if len(declared) > maxDetailPages {
		return nil, fmt.Errorf("poll %s: %d detail pages exceeds limit %d", source.ID, len(declared), maxDetailPages)
	}
	base, err := url.Parse(source.URL)
	if err != nil {
		return nil, err
	}

	// Validate the complete request set before fetching anything: a collection
	// with a forbidden link must not spend requests on the allowed ones.
	targets := make([]string, 0, len(declared))
	seen := make(map[string]bool, len(declared))
	for _, raw := range declared {
		resolved, err := detailURL(base, raw)
		if err != nil {
			return nil, fmt.Errorf("poll %s: %w", source.ID, err)
		}
		if seen[resolved] {
			return nil, fmt.Errorf("poll %s: duplicate detail URL %s", source.ID, resolved)
		}
		seen[resolved] = true
		targets = append(targets, resolved)
	}

	details := make([]Page, 0, len(targets))
	for _, target := range targets {
		fetched, err := p.Fetcher.Fetch(ctx, target, FetchCache{})
		if err != nil {
			return nil, err
		}
		switch fetched.Status {
		case FetchModified:
			if fetched.Page == nil {
				return nil, fmt.Errorf("poll %s: detail %s returned no page", source.ID, target)
			}
			details = append(details, fetched.Page)
		case FetchThrottled:
			return nil, &fetchThrottle{fetched.RetryAfter}
		default:
			return nil, fmt.Errorf("poll %s: detail %s unavailable (status %d)", source.ID, target, fetched.Status)
		}
	}
	return flow.ExtractDetails(collection, details)
}

// detailURL resolves a declared detail link against the Source URL and
// constrains the result to that URL's HTTPS origin.
func detailURL(base *url.URL, raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("invalid detail URL %q", raw)
	}
	resolved := base.ResolveReference(parsed)
	if resolved.Scheme != "https" {
		return "", fmt.Errorf("detail URL %s is not HTTPS", resolved)
	}
	if !strings.EqualFold(resolved.Host, base.Host) {
		return "", fmt.Errorf("detail URL %s leaves origin %s", resolved, base.Host)
	}
	return resolved.String(), nil
}
