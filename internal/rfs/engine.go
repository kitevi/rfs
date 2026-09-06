package rfs

import (
	"context"
	"fmt"
	"time"
)

type PageFetcher interface {
	Fetch(context.Context, string, FetchCache) (FetchResult, error)
}

type SnapshotStore interface {
	LoadFetchCache(context.Context, string) (FetchCache, error)
	SaveFetchCache(context.Context, string, FetchCache) error
	SaveSnapshot(context.Context, string, []Item) error
	MergeHistory(context.Context, string, []Item, []string, int) error
	FirstSeen(context.Context, string, string, time.Time) (time.Time, error)
}

type Clock interface {
	Now() time.Time
}

type Poller struct {
	Fetcher PageFetcher
	Store   SnapshotStore
	Clock   Clock
}

type PollStatus int

const (
	PollUpdated PollStatus = iota
	PollUnchanged
	PollThrottled
)

type PollResult struct {
	Status     PollStatus
	RetryAfter time.Duration
}

func (p Poller) Poll(ctx context.Context, source Source) (PollResult, error) {
	if p.Fetcher == nil {
		return PollResult{}, fmt.Errorf("poll %s: missing fetcher", source.ID)
	}
	if p.Store == nil {
		return PollResult{}, fmt.Errorf("poll %s: missing store", source.ID)
	}
	if source.Flow == nil {
		return PollResult{}, fmt.Errorf("poll %s: missing flow", source.ID)
	}

	cache, err := p.Store.LoadFetchCache(ctx, source.ID)
	if err != nil {
		return PollResult{}, err
	}

	// A snapshot is derived data: a function of (page bytes, extraction code).
	// An HTTP 304 only proves the page bytes are unchanged — not that the
	// parser is. When the running Flow's version differs from the version that
	// produced the stored snapshot, drop the conditional headers for this one
	// fetch so the server returns the full page and Extract re-runs against
	// it, overwriting the stale snapshot below.
	requestCache := cache
	if source.Flow.Version() != cache.ExtractVersion {
		requestCache = FetchCache{}
	}

	fetchResult, err := p.Fetcher.Fetch(ctx, source.URL, requestCache)
	if err != nil {
		return PollResult{}, err
	}

	// A 304 re-derives nothing, so preserve the stored version (it still
	// describes the snapshot on disk). Only a real re-derivation advances it.
	savedCache := fetchResult.Cache
	savedCache.ExtractVersion = cache.ExtractVersion

	switch fetchResult.Status {
	case FetchNotModified:
		if err := p.Store.SaveFetchCache(ctx, source.ID, savedCache); err != nil {
			return PollResult{}, err
		}
		return PollResult{Status: PollUnchanged}, nil
	case FetchThrottled:
		return PollResult{Status: PollThrottled, RetryAfter: fetchResult.RetryAfter}, nil
	case FetchModified:
		if fetchResult.Page == nil {
			return PollResult{}, fmt.Errorf("poll %s: modified fetch returned no page", source.ID)
		}
	default:
		return PollResult{}, fmt.Errorf("poll %s: unknown fetch status %d", source.ID, fetchResult.Status)
	}

	extracted, err := source.Flow.Extract(fetchResult.Page)
	if err != nil {
		return PollResult{}, err
	}

	items := make([]Item, 0, len(extracted))
	seenGUIDs := map[string]struct{}{}
	for _, item := range extracted {
		if item.GUID == "" {
			continue
		}
		if _, seen := seenGUIDs[item.GUID]; seen {
			continue
		}
		seenGUIDs[item.GUID] = struct{}{}
		pubDate := p.now()
		if item.PubDate != nil {
			pubDate = *item.PubDate
		} else {
			seenAt, err := p.Store.FirstSeen(ctx, source.ID, item.GUID, pubDate)
			if err != nil {
				return PollResult{}, err
			}
			pubDate = seenAt
		}
		items = append(items, Item{
			GUID:        item.GUID,
			Title:       item.Title,
			Link:        item.Link,
			Description: item.Description,
			PubDate:     pubDate,
			Replies:     item.Replies,
		})
	}

	if source.History != nil {
		keepStored := source.History.StoredLimit
		if keepStored <= 0 {
			keepStored = 11
		}
		liveGUIDs := make([]string, 0, len(items))
		for _, item := range items {
			liveGUIDs = append(liveGUIDs, item.GUID)
		}
		if err := p.Store.MergeHistory(ctx, source.ID, items, liveGUIDs, keepStored); err != nil {
			return PollResult{}, err
		}
	} else if err := p.Store.SaveSnapshot(ctx, source.ID, items); err != nil {
		return PollResult{}, err
	}
	// The snapshot was just re-derived with the running Flow's code, so advance
	// the stored version — future polls can trust a 304 until this changes again.
	savedCache.ExtractVersion = source.Flow.Version()
	if err := p.Store.SaveFetchCache(ctx, source.ID, savedCache); err != nil {
		return PollResult{}, err
	}
	return PollResult{Status: PollUpdated}, nil
}

func (p Poller) now() time.Time {
	if p.Clock == nil {
		return time.Now().UTC()
	}
	return p.Clock.Now()
}

var _ Flow = flowFunc(nil)

type flowFunc func(Page) ([]ExtractedItem, error)

func (f flowFunc) Extract(page Page) ([]ExtractedItem, error) { return f(page) }

// Version is 0 for ad-hoc flowFuncs, so they never trigger a version-driven
// re-derive. Named Flows declare their own version (see meltzer.Flow).
func (flowFunc) Version() int { return 0 }
