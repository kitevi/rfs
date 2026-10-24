# Projection items can be dated from their own pages

Amends 0001 for projection Sources whose collection lists items without dates.

The Coached essay index names each essay but does not date it, so every essay
was dated by when rfs first saw it. In October 2026 the site moved two essays
from August and September to new slugs. The GUID comes from the slug, so both
came back as new items dated that week and sat at the top of the feed.

## Decision

- A projection Flow can implement `DatedItemFlow`: it names an item's own page
  and reads a date from it. rfs does the fetching, routed through the domain
  gate like every other follow-up request (0002).
- rfs saves each date it reads in `item_dates` and fetches an item's page only
  until it has a date. The table repeats what the source still publishes, so
  it is a cache: losing it costs refetches, not feed content, and the feed stays
  reconstructable from current upstream state.
- An item page that cannot be read leaves the item undated for that poll, so the
  first-seen fallback applies (0011). The poll drops the collection's
  validators, so the next poll fetches the collection in full and retries the
  page instead of stopping at a 304. Throttling and cancellation still abort
  the poll.

## Consequences

- A moved item still has a new GUID, so a reader counts it as unread. Its date
  is the original one, so it sorts among older items instead of at the top.
- The first poll after a Flow adopts this fetches one page per item. Coached:
  47 pages, about eight minutes at the default 10-second domain spacing.
