package rfs

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"slices"
)

// EnrichedChangeFlow describes an optional metadata query for a batch of
// emitted changes. rfs performs the GET; the Flow builds the URL and decodes
// the response. An empty URL leaves the batch as it is.
type EnrichedChangeFlow interface {
	ChangeFlow
	EnrichmentURL([]ExtractedItem) (string, error)
	Enrich(Page, []ExtractedItem) ([]ExtractedItem, error)
}

func (p Poller) enrichChanges(ctx context.Context, flow Flow, changes []ExtractedItem) ([]ExtractedItem, error) {
	enriched, ok := flow.(EnrichedChangeFlow)
	if !ok {
		return changes, nil
	}
	for batch := range slices.Chunk(changes, 50) {
		url, err := enriched.EnrichmentURL(batch)
		if err != nil {
			return nil, err
		}
		if url == "" {
			continue
		}
		page, err := p.fetchFollowUp(ctx, url, "metadata")
		if err != nil {
			return nil, err
		}
		items, err := enriched.Enrich(page, batch)
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

// AnnouncementIdentityFlow rewrites an extracted item to the release identity
// Evaluate records. rfs fetches; the Flow only reads the response. Items whose
// identity is already final are not fetched.
type AnnouncementIdentityFlow interface {
	AnnouncementFlow

	// PlanIdentities applies identities this checkpoint already resolved and
	// returns items that still need a player page, in observation order. ready
	// preserves that order and still contains each pending item unchanged, so
	// the resolved item can replace it.
	PlanIdentities(checkpoint json.RawMessage, current []ExtractedItem) ([]ExtractedItem, []ExtractedItem, error)

	// IdentityURL is the player page for one pending item.
	IdentityURL(ExtractedItem) (string, error)

	// ResolveIdentity returns the item Evaluate should see. It may replace the
	// GUID with the release the player belongs to.
	ResolveIdentity(Page, ExtractedItem) (ExtractedItem, error)
}

// resolveAnnouncementIdentities fetches player pages for items whose release
// identity is not yet final. An item whose page cannot be read is quarantined
// out of the observation, so one broken player cannot stop the release feed: the
// checkpoint does not record it and the next poll retries it.
func (p Poller) resolveAnnouncementIdentities(ctx context.Context, sourceID string, flow AnnouncementFlow, checkpoint json.RawMessage, current []ExtractedItem) ([]ExtractedItem, error) {
	resolver, ok := flow.(AnnouncementIdentityFlow)
	if !ok || len(current) == 0 {
		return current, nil
	}
	ready, pending, err := resolver.PlanIdentities(checkpoint, current)
	if err != nil {
		return nil, err
	}
	for _, item := range pending {
		url, err := resolver.IdentityURL(item)
		if err != nil {
			return nil, err
		}
		if url == "" {
			return nil, fmt.Errorf("announcement identity missing a player URL")
		}
		resolved, err := p.resolveIdentity(ctx, resolver, url, item)
		if err != nil {
			if !quarantinable(err) {
				return nil, err
			}
			log.Printf("rfs: quarantine %s/%s: %v", sourceID, item.GUID, err)
			ready = withoutItems(ready, []ExtractedItem{item})
			continue
		}
		found := false
		for i := range ready {
			if ready[i].GUID == item.GUID {
				ready[i] = resolved
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("announcement identity %q was not in the observation", item.GUID)
		}
	}
	return ready, nil
}

// resolveIdentity fetches one identity page and lets the Flow read it.
func (p Poller) resolveIdentity(ctx context.Context, resolver AnnouncementIdentityFlow, url string, item ExtractedItem) (ExtractedItem, error) {
	page, err := p.fetchFollowUp(ctx, url, "announcement identity")
	if err != nil {
		return ExtractedItem{}, err
	}
	return resolver.ResolveIdentity(page, item)
}

// enrichAnnouncements resolves metadata for newly announced items before they
// are committed. Requests run sequentially, so a whole-page replacement cannot
// burst at the upstream host. An item whose metadata cannot be read is returned
// as quarantined for the caller to drop from the observation: the rest of the
// batch publishes, and the checkpoint never books the skipped item, so the next
// poll retries it. A Flow that renames an identity is a contract violation and
// stays fatal.
func (p Poller) enrichAnnouncements(ctx context.Context, sourceID string, flow Flow, announcements []ExtractedItem, resolved map[string]ExtractedItem) ([]ExtractedItem, []ExtractedItem, error) {
	enriched, ok := flow.(EnrichedAnnouncementFlow)
	if !ok || len(announcements) == 0 {
		return announcements, nil, nil
	}
	published := make([]ExtractedItem, 0, len(announcements))
	var quarantined []ExtractedItem
	for _, item := range announcements {
		if cached, ok := resolved[item.GUID]; ok {
			published = append(published, cached)
			continue
		}
		url, err := enriched.AnnouncementEnrichmentURL(item)
		if err != nil {
			return nil, nil, err
		}
		if url == "" {
			published = append(published, item)
			continue
		}
		candidate, err := p.fetchAnnouncement(ctx, enriched, url, item)
		if err != nil {
			if !quarantinable(err) {
				return nil, nil, err
			}
			log.Printf("rfs: quarantine %s/%s: %v", sourceID, item.GUID, err)
			quarantined = append(quarantined, item)
			continue
		}
		if candidate.GUID != item.GUID {
			return nil, nil, fmt.Errorf("announcement metadata changed GUID %q to %q", item.GUID, candidate.GUID)
		}
		resolved[item.GUID] = candidate
		published = append(published, candidate)
	}
	return published, quarantined, nil
}

// fetchAnnouncement fetches one announcement's metadata page and lets the Flow
// decode it.
func (p Poller) fetchAnnouncement(ctx context.Context, flow EnrichedAnnouncementFlow, url string, item ExtractedItem) (ExtractedItem, error) {
	page, err := p.fetchFollowUp(ctx, url, "announcement metadata")
	if err != nil {
		return ExtractedItem{}, err
	}
	return flow.EnrichAnnouncement(page, item)
}
