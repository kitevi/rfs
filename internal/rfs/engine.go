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
	CommitHistory(context.Context, string, []Item, []string, int, FetchCache, HistoryRebuilderFunc) error
	LoadLiveGUIDs(context.Context, string) ([]string, error)
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
	versionChanged := source.Flow.Version() != cache.ExtractVersion
	requestCache := cache
	if versionChanged {
		requestCache = FetchCache{}
	}

	// A collection validator only proves that the collection bytes are
	// unchanged. It says nothing about the additional pages that carry observed
	// state, so those Flows always fetch the collection unconditionally.
	if flowRequiresFullPage(source.Flow) {
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
		if flowRequiresFullPage(source.Flow) {
			return PollResult{}, fmt.Errorf("poll %s: requires a full collection page, got 304", source.ID)
		}
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

	extracted, err := p.extractObservation(ctx, source, fetchResult.Page)
	if err != nil {
		return pollFailure(err)
	}

	if comparesChanges(source.Flow) {
		return p.pollChanges(ctx, source, source.Flow, extracted, savedCache)
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
			Metadata:    item.Metadata,
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
		if len(liveGUIDs) == 0 {
			// An empty successful observation (a parseable catalog gap) must
			// not wipe the stored live set; preserve it so pruning and
			// visibility survive until the next poll heals.
			kept, err := p.Store.LoadLiveGUIDs(ctx, source.ID)
			if err != nil {
				return PollResult{}, err
			}
			liveGUIDs = kept
		}
		var rebuild HistoryRebuilderFunc
		if versionChanged {
			if flow, ok := source.Flow.(HistoryRebuilder); ok {
				rebuild = flow.RebuildStored
			}
		}
		savedCache.ExtractVersion = source.Flow.Version()
		if err := p.Store.CommitHistory(ctx, source.ID, items, liveGUIDs, keepStored, savedCache, rebuild); err != nil {
			return PollResult{}, err
		}
		return PollResult{Status: PollUpdated}, nil
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

// extractObservation derives items for a Source, fetching any additional pages
// its Flow declares before extraction.
func (p Poller) extractObservation(ctx context.Context, source Source, first Page) ([]ExtractedItem, error) {
	if flow, ok := source.Flow.(PaginatedFlow); ok {
		return p.extractPages(ctx, source, flow, first)
	}
	return source.Flow.Extract(first)
}

// flowRequiresFullPage reports whether a Flow's observed state depends on pages
// that a conditional answer for the collection cannot refresh.
func flowRequiresFullPage(flow Flow) bool {
	_, ok := flow.(PaginatedFlow)
	return ok
}

func (p Poller) now() time.Time {
	if p.Clock == nil {
		return time.Now().UTC()
	}
	return p.Clock.Now()
}
