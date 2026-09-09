# Stateful recommendation diffs with complete collection fetches

Amends ADR 0001 for Sources whose Flow implements `ChangeFlow`. Existing
projection and catalog-history Sources keep their current behavior.

SeaDex's Discord notifications show field-level recommendation diffs, not merely
the latest torrent list. Its public API exposes current entries and their related
torrents, not a revision log. A GUID based only on an anime ID hides edits; a
content hash alone loses repeated transitions such as A → B → A → B.

## Decision

- `ChangeFlow.Extract` emits stable entity identities with serialized comparison
  state in the opaque description. `Changes(previous, current)` emits only
  observed differences, with display-ready descriptions. Both remain pure.
- A first successful complete observation establishes a silent baseline. An
  extraction-version mismatch also silently rebaselines, preserving prior feed
  items without misreporting extraction-code changes as upstream edits.
- SQLite stores the baseline and a monotonically increasing poll revision in
  `change_state`. Change GUIDs combine source ID, revision, and entity identity.
  Baseline, emitted items and fetch validators commit in one transaction.
- Feed items are dated at observation time and retained in `snapshots` across
  restarts. They are not overwritten by later revisions. There is currently no
  pruning policy for change feeds; disk use and feed size grow with history.

## Complete collections

`PaginatedFlow` declares numbered collection metadata. rfs, not the Flow,
fetches subsequent pages with the shared HTTP fetcher. It checks page numbers,
consistent totals, final item count and unique identities before comparing any
state. SeaDex also validates entity IDs and expanded torrent relations.

SeaDex requests 500 entries per page in stable ID order, excluding large file
lists. A poll does not reuse a validator for page one: a 304 there cannot prove
that other pages or expanded relations are unchanged. Invalid, failed or
throttled pages leave the baseline and feed untouched. Secondary requests
propagate Retry-After to the existing poll gate.

These checks cannot make the upstream API transactional. Concurrent upstream
edits may still cross page boundaries without changing totals; the next poll
reconciles observed state. Intermediate edits and edits reverted between polls
cannot be reconstructed.

## Presentation and enrichment

SeaDex compares Best, Alt, Unmuxed Best, Notes, Tags, Dual Audio, Incomplete,
Comparisons and release identities/links. Set-like fields are canonicalized;
text diffs preserve line order and repetitions and trim common leading/trailing
lines. Only changed sections are emitted. Descriptions contain escaped text with
HTML headings, preformatted `-`/`+` lines and optional colors.

`EnrichedChangeFlow` describes read-only JSON metadata queries; rfs performs
batched requests (up to 50 changed items) through the same HTTP client. SeaDex
uses SeaDex's public `anilist` collection via GET for titles and cover URLs.
A nil enrichment request body selects GET; non-nil bodies retain JSON POST.
The original direct AniList POST returned HTTP 403 on this host, preventing
all changed polls from committing. The SeaDex-hosted cache was verified live;
no blocked endpoint is bypassed.
No metadata requests are made for the baseline or unchanged polls. A failed
query preserves the pending change for retry. Missing titles fall back to
AniList IDs; missing/invalid cover URLs are ignored. rfs never fetches images.

`SourceMeta.ItemDescriptionsHTML` opts the browser view into rendering
Flow-generated HTML. It is enabled only for SeaDex, whose renderer escapes all
upstream text and permits only HTTP(S) cover URLs. Other Sources retain escaped
plain-text descriptions. RSS description content uses normal XML escaping.

## Alternatives

- Native torrent RSS: does not provide the required before/after recommendation
  descriptions or guarantee notifications for edits to existing items.
- Poll only the newest entries: can silently omit older edited relations and
  deletions; fetch the entire collection instead.
- Let a Flow fetch pages or keep its own in-memory baseline: violates ADRs 0002
  and 0005, and loses state on restart.
- Scrape Discord or bypass API access blocks: not needed for this design and not
  implemented. Normal API access must be available on the deployed host.
