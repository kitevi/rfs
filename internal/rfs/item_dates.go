package rfs

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"
)

// DatedItemFlow is a projection Flow whose collection names items without
// dating them, while each item's own page carries the date the source gives
// it. rfs fetches an item's page once, remembers the date, and falls back to
// first-seen time only while that page cannot be read. The Flow builds the URL
// and reads the response; neither method performs IO.
type DatedItemFlow interface {
	Flow

	// ItemDateURL names the page that dates one undated item.
	ItemDateURL(ExtractedItem) (string, error)

	// ItemDate reads the source's date for the item from its page.
	ItemDate(Page, ExtractedItem) (time.Time, error)
}

// ItemDateStore remembers dates read from item pages. Each one restates what
// the source still publishes, so the store is a cache: losing it costs
// refetches, not feed content.
type ItemDateStore interface {
	LoadItemDate(ctx context.Context, sourceID, guid string) (time.Time, bool, error)
	SaveItemDate(ctx context.Context, sourceID, guid string, date time.Time) error
}

// dateItems fills in source dates for undated items of a DatedItemFlow. An
// item whose page cannot be read keeps no date, so the first-seen fallback
// applies for this poll, and pending reports that the next poll should retry
// it. A date is saved as soon as it is read, so a poll cut short keeps the
// dates it already fetched.
func (p Poller) dateItems(ctx context.Context, source Source, items []ExtractedItem) (dated []ExtractedItem, pending bool, err error) {
	flow, ok := source.Flow.(DatedItemFlow)
	if !ok {
		return items, false, nil
	}
	store, ok := p.Store.(ItemDateStore)
	if !ok {
		return nil, false, fmt.Errorf("poll %s: store does not support item dates", source.ID)
	}
	dated = make([]ExtractedItem, len(items))
	copy(dated, items)
	for i, item := range dated {
		if item.GUID == "" || item.PubDate != nil {
			continue
		}
		date, found, err := store.LoadItemDate(ctx, source.ID, item.GUID)
		if err != nil {
			return nil, false, err
		}
		if !found {
			date, err = p.fetchItemDate(ctx, flow, item)
			if err != nil {
				if !quarantinable(err) {
					return nil, false, err
				}
				log.Printf("rfs: undated %s/%s: %v", source.ID, item.GUID, err)
				pending = true
				continue
			}
			if err := store.SaveItemDate(ctx, source.ID, item.GUID, date); err != nil {
				return nil, false, err
			}
		}
		dated[i].PubDate = &date
	}
	return dated, pending, nil
}

// fetchItemDate fetches one item's page and lets the Flow read its date.
func (p Poller) fetchItemDate(ctx context.Context, flow DatedItemFlow, item ExtractedItem) (time.Time, error) {
	url, err := flow.ItemDateURL(item)
	if err != nil {
		return time.Time{}, err
	}
	if err := p.beforeSecondaryFetch(ctx, url); err != nil {
		return time.Time{}, err
	}
	result, err := p.Fetcher.Fetch(ctx, url, FetchCache{})
	if err != nil {
		return time.Time{}, err
	}
	if result.Status == FetchThrottled {
		return time.Time{}, &fetchThrottle{result.RetryAfter}
	}
	if result.Status != FetchModified {
		return time.Time{}, fmt.Errorf("item page unavailable (status %d)", result.Status)
	}
	return flow.ItemDate(result.Page, item)
}

func (s *SQLiteStore) LoadItemDate(ctx context.Context, sourceID, guid string) (time.Time, bool, error) {
	var value string
	err := s.db.QueryRowContext(ctx, `SELECT pub_date FROM item_dates WHERE source_id = ? AND guid = ?`, sourceID, guid).Scan(&value)
	if err == sql.ErrNoRows {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	date, err := parseStoreTime(value)
	return date, err == nil, err
}

func (s *SQLiteStore) SaveItemDate(ctx context.Context, sourceID, guid string, date time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO item_dates (source_id, guid, pub_date) VALUES (?, ?, ?)
		ON CONFLICT(source_id, guid) DO UPDATE SET pub_date = excluded.pub_date`, sourceID, guid, formatStoreTime(date))
	return err
}
