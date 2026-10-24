# Feeds are projections of the source's current state, not additions logs

> **Status: amended.** This still describes projection Sources (Meltzer,
> Coached). Catalog history (0007), change feeds (0008) and announcement
> checkpoints (0010) depend on stored state by design; read those for the
> other Sources.

Each source's RSS feed is rebuilt from the source's *current* content on every poll and reflects the items currently present — it is not an append-only log of additions rfs has detected over time. Item pubDates come from the source itself (e.g. a match date on a Wikipedia list), falling back to rfs's discovery time only when the Flow can't extract a date.

We chose this so the feed is reconstructable from the source's current state alone, with no dependence on stored history. Additions are detected by RSS readers via item GUIDs (the standard RSS model), and rfs never synthesizes edit events, so additions-only behaviour falls out for free.

## Considered options

- **Additions-log feed with discovery-time pubDate** (rejected): rfs would store every item it has ever detected as new and serve a growing log; pubDate = when rfs first saw the item. Guarantees new additions surface at the top of feed readers, but makes the feed dependent on stored history — it can't be rebuilt from the current page. Rejected because reconstructability from current state was the driving requirement.

## Consequences

- A newly-added item whose source date is in the past (e.g. a 1982 match added to the list in 2026) is dated 1982 in the feed. Most RSS readers recognise it as new by GUID but sort it by pubDate, so it may be buried or auto-marked-read rather than prominently surfaced. Accepted as the deliberate price of reconstructability.
- Storage holds a per-source current snapshot plus a first-seen map (GUID → time) only for the discovery-time fallback — not an append-only history of all items ever emitted.
