# Quarantine unreadable items instead of wedging a feed

Amends 0008, 0009 and 0010 for Sources whose Flow announces items.

A per-item failure used to abort the whole poll: one chapter whose heading
disagreed with its link, one film page without a day button, or one Bandcamp
player that would not load. The commit never happened, so the checkpoint never
advanced, and the next poll read the same page, hit the same item, and failed
the same way. The archive and the stack page are cumulative — the bad item stays
listed — so the failure was permanent. The stored snapshot kept being served
with HTTP 200, so subscribers saw a feed that silently stopped updating, and the
domain gate booked every failure as throttling.

## Decision

- A Flow skips an item it cannot read when the rest of the page is usable, and
  still fails closed when nothing on the page is usable. One Piece skips a
  chapter whose link and heading disagree (logged) and rejects a page whose
  only chapter disagrees.
- The poller quarantines a per-item failure in the announcement pipeline: the
  item is removed from this poll's observation, the rest of the observation is
  decided and published, and the item is retried on the next poll.
- Enrichment runs before the decision is committed. When an announcement cannot
  be enriched, the decision is re-derived from the same checkpoint with the
  quarantined item removed, so the checkpoint never books an item the feed did
  not publish. Each pass removes at least one item, so the loop is bounded by
  the observation size.
- Throttling and context cancellation stay source-wide and abort the poll, so
  Retry-After still reaches the domain gate. A Flow that changes an item's GUID
  during enrichment is a contract violation, not an unreachable item, and stays
  fatal.
- Change feeds keep all-or-nothing enrichment. Their items are diffs recomputed
  from the current state on every poll, so a retry loses nothing, and 0008
  documents that failed metadata preserves the baseline and feed. Quarantining a
  diff would silently drop an observed edit.

## Considered options

- **Fail closed per page** (rejected): that was the bug. It trades one bad item
  for the whole feed, forever.
- **Drop the item silently** (rejected): a quarantined release would never be
  announced, which is the outcome 0010 exists to prevent.
- **Advance the checkpoint over the failed item** (rejected): the checkpoint
  would claim an entry the feed never published, and nothing would retry it.
- **A durable per-item retry queue with its own backoff** (rejected): new state,
  new migrations, and a second source of truth. The upstream page is already the
  queue: the item comes back on the next poll.

## Consequences

- A permanently broken item costs its own entry, not the feed. The log names the
  source and GUID, and the next poll retries it without operator action.
- A poll that quarantines part of its observation publishes a partial batch.
  Accepted: the alternative is publishing nothing until upstream fixes itself,
  which for a cumulative archive means never.
- Quarantine is per poll, not per database row, so it needs no schema change.

## Verification

Tests cover extraction skipping (One Piece), identity quarantine with a later
retry (`internal/rfs`), enrichment quarantine with a later retry (`internal/rfs`,
A Closer Listen), and that throttling, cancellation, and a renamed identity
still abort the poll.
