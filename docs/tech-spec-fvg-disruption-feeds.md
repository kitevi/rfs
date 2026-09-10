# FVG operator-confirmed disruption feeds

Status: partially implemented. The rail scope, the terminal states and the
extraction-version upgrade shipped; see
`docs/adr/0010-broadened-disruption-scope.md` for the decisions and the
implementation notes at the end of this document for what is verified, what
discovery found and what is still blocked.

## Goal and scope

Publish every operator-confirmed disruption that reaches travel in Friuli Venezia Giulia, from the operator that publishes it. It replaces the strike-only scope these feeds started with.

Include delays and diversions, not only complete unavailability. The rail feed keeps its stored history; buses become one feed per operator

| Source ID | Coverage |
| --- | --- |
| `arriva-udine` | the services Arriva Udine runs, as its own notice endpoint publishes them |
| `trieste-trasporti` | the notices Trieste Trasporti lists as in force |
| `apt-gorizia` | the service notices and route diversions APT Gorizia lists as in force |
| `trenitalia-disruptions` | Trenitalia passenger services affecting travel in Friuli Venezia Giulia, including applicable national notices |

Naming follows the scope: the feeds, their paths and the Go packages use
disruption vocabulary rather than `scioperi`, which stays only where it is
upstream data. The rename changes the feed URLs, so a subscriber has to
subscribe again; see `docs/adr/0010-per-operator-bus-feeds.md`.
“All disruptions” means all qualifying notices discoverable in the verified official collections, not exhaustive knowledge of every departure. No claim of complete transport coverage is permitted.

## Eligibility

Publish an official notice that establishes both covered-service applicability and an operational impact, whether actual, anticipated or conditional.

Include:

- Strikes with established potential passenger-service impact.
- Weather, technical faults, accidents, infrastructure damage and emergency interventions causing disruption.
- Suspensions, cancellations, delays, route limitations, diversions and replacement transport.
- Temporary stop closures or relocations and service restrictions.
- Planned works or temporary timetable changes that alter covered journeys; future announced restrictions qualify before their start date.
- Meaningful updates and explicit restoration or withdrawal of previously observed disruptions.

Exclude generic news, promotional content, ordinary permanent timetable publications, standing information/guarantee pages without a specific disruption, unrelated regions/operators, ATAP-only notices, freight-only incidents and maintenance without stated passenger impact. Preserve existing strike coverage, including nationally applicable notices without city keywords.

Bus operator scope must come from the affected services, not incidental operator mentions. Rail scope can come from FVG tags, explicit covered routes or a national statement encompassing FVG. Routes through Udine, Trieste, Gorizia or Monfalcone qualify even if tagged only with a neighbouring region. Do not classify notices by a cause keyword alone: weather news without service impact does not qualify.

Keep the operator's distinction between “may be cancelled” and “is cancelled”. An incident-level feed item does not assert that every train or bus on a route is cancelled.

### Required example

The 10 September 2026 “Linea Venezia - Trieste: circolazione rallentata dalle ore 15:30 per condizioni meteo critiche” notice qualifies as an FVG rail disruption. Preserve its latest reported impact and supporting original text, including delay estimates and the explicit IC 584 cancellation. Subsequent meaningful edits to the same incident produce revision items, not unrelated incidents.

## Upstream collections

Rail reads Trenitalia's official Infomobilità page,
`https://www.trenitalia.com/it/informazioni/Infomobilita/notizie-infomobilita.html`.
The captured September 10 fixture carries the required weather bulletin, and the
live probe reports the same notices against the current page.

Each bus feed reads the collection its own operator publishes: Arriva Udine's
notice endpoint, Trieste Trasporti's notice page, and APT Gorizia's summary of
the changes in force. TPL FVG's alert hub
(`https://tplfvg.it/it/infomobilita/avvisi-sul-servizio/`) is prose plus links
rather than a notice collection, which is why the buses are one feed per operator
instead of one feed over three origins.

Every collection was captured into a fixture and checked against the live site
before it was implemented, and the fixture notes record what each capture
contains. A fixture is evidence for the markup rfs reads, never a claim that a
collection is complete.

## Observation and fetching architecture

Continue using `ChangeFlow`, complete observations and atomic SQLite baseline/history persistence. Keep extraction/classification pure; all network access belongs to rfs and uses its shared fetcher, domain gate, timeouts and throttling.

A Flow reads only the bytes rfs fetched for its own Source URL, so a collection on another origin needs its own Source with its own poll rather than an engine extension. Do not hide HTTP calls inside a Flow or weaken same-origin checks globally.

The extension must provide a bounded engine-owned fetch plan, validated exact HTTPS origin allowlists, redirect validation, pagination-cycle detection and explicit request limits. Resolve concrete limits from the discovery matrix before implementation. Refresh required detail bodies even if collection validators are unchanged. Existing single-origin Sources must retain their behavior and restrictions.

Any required collection/detail/page failure, malformed content, block, exceeded limit or incomplete traversal fails that Source's poll and preserves its previous baseline and history. An empty collection is valid only when its expected structure and explicit empty semantics are recognized. Bus failure does not prevent independent rail polling.

## Identity, comparison and presentation

Use the verified upstream immutable identifier or canonical notice permalink. Namespace IDs by publisher/collection where necessary. Never derive identity from mutable titles, delay values, affected stops, dates or body hashes. If upstream reuses component IDs, discovery must establish a stable discriminator before implementation; do not silently merge separate incidents.

Prefer one authoritative collection when the same bus notice is syndicated. Otherwise deduplicate only through proven shared IDs/canonical links, not fuzzy title matching. Record unresolved upstream duplication as a limitation.

Store a versioned normalized payload containing original title/body, source link, scope, operator/route information where available, upstream publication/update times, validity information, lifecycle state and supporting links. Optional fields remain unknown when upstream does not establish them. Avoid speculative natural-language date inference.

Normalize markup, whitespace and safe URL representation. Preserve operational wording, times, stops and train identifiers. Compare the full substantive notice so an unrecognized operational edit is not discarded. Navigation, share widgets and formatting changes are not substantive; a publication timestamp-only edit emits nothing. Explicit validity-window changes do emit.

Use source-faithful Italian descriptions and titles such as:

- `[Treni · FVG] Linea Venezia - Trieste: ...`
- `[Aggiornato · Treni · FVG] Linea Venezia - Trieste: ...`
- `[Bus · Arriva Udine] ...`
- `[Ripristinato · Treni · FVG] ...`

An update includes current operator text and a focused change summary/diff. A partial restoration remains active and is an update, not a full restoration. “Circolazione regolare” inside an old embedded bulletin must not override a newer active update. Use the latest authoritative status only; ambiguous status remains unknown rather than resolved.

RSS dates remain observation times; upstream times and validity windows belong in descriptions. The bus feeds narrow this to new items whose page states a reliable publication time — see `docs/adr/0011-bus-notice-dates-and-expiry.md`. Retain revision-qualified GUIDs, including A → B → A transitions. Escape operator text in RSS/HTML and reject unsafe supporting-link schemes.

## Lifecycle

| Observation | Publication |
| --- | --- |
| First observation of active or announced future disruption | Publish current notice |
| First observation of already resolved, withdrawn or expired notice | Store if needed for comparison; no historical alert |
| Unchanged or formatting-only notice | Nothing |
| Material edit to known active disruption | Updated item |
| Explicit full restoration of known active disruption | Restoration item |
| Explicit revocation/withdrawal of known disruption | Revocation/withdrawal item using source wording |
| Partial restoration with residual restrictions | Updated active item |
| Disappearance or confidently established expiry | No inferred restoration or cancellation |
| Explicit renewed disruption on a retained resolved identity | New revision item |

Keep terminal notices in comparison state while upstream exposes them so repeated terminal polls do not duplicate notifications. This proposal does not add indefinite tombstones: after a notice disappears from the stored observation, a later active reappearance is a new observation. Document that limitation.

Do not use the notice publication date as an expiry date. For explicit validity windows, use Europe/Rome and test daylight-saving boundaries. Ambiguous validity is not grounds for suppressing a currently listed disruption. The bus feeds add a separate two-calendar-month freshness rule on the publication date, which suppresses the announcement without claiming the disruption ended — see `docs/adr/0011-bus-notice-dates-and-expiry.md`. Past revisions remain in feed history; this is not a live departure board.

## Upgrade behavior

New installations retain `EmitInitial: true`. Existing subscribers must also receive currently active non-strike disruptions when this broader scope is deployed, without replaying unchanged strikes or historical feed items.

The existing extraction-version mechanism silently rebaselines, so a version bump alone does not satisfy this requirement. Add a narrowly scoped, version-gated migration for these two Sources, documented in an ADR:

1. Fetch and extract a complete broader observation; never migrate on a partial poll.
2. Convert the previous strike payloads to the new comparison schema while retaining entity identity.
3. Compare against the broader observation: emit newly eligible active/future disruptions, meaningful existing-notice changes and explicit terminal transitions of known active notices. Do not announce newly discovered terminal notices.
4. Atomically commit converted baseline, new extraction version and emitted revisions using existing revision bookkeeping.
5. Retry safely after failure; restart after a successful migration emits no duplicates.

If no valid compatible prior snapshot exists, initialize from the current complete observation and publish its active/future notices; document possible repeats after state loss. Do not reset the database or rewrite historical GUIDs. Unrelated Sources, especially SeaDex, retain silent version-rebaseline semantics. A later unsupported version transition retains the default behavior unless explicitly migrated.

## Polling and limits

Propose a source-specific 10-minute interval for both transport Sources using the existing interval facility; no new CLI flag. Confirm upstream request budgets after discovery, especially bus detail fan-out, and document any necessary slower interval before release. Domain throttling and Retry-After take precedence.

These are observed-change feeds, not instant emergency alerts. Changes occurring and reverting between polls may be missed. No automatic history pruning, reminders or push delivery is added.

## Implementation

- `internal/sources/trenitalia` reads the rail collection.
- `internal/sources/notices` owns the shared bus behaviour: the notice permalink
  is the identity, an edit emits an update with the same identity, and a notice
  that leaves the list emits nothing.
- `internal/sources/arrivaudine`, `internal/sources/triestetrasporti` and
  `internal/sources/aptgorizia` decode one collection each, and each fails the
  poll when its page no longer has the structure it expects.
- A live probe covers every transport feed:
  `RFS_TEST_TRANSPORT_LIVE=1 go test ./internal/sources -run TestLiveTransportFeeds -v`.

## Acceptance ledger

- [ ] The rail feed and the three bus feeds exist, and each renders through its RSS and HTML route.
- [ ] The real September 10 Venezia–Trieste fixture emits the weather disruption, preserving FVG applicability and original operational wording. An updated variant emits exactly one revision.
- [ ] Rail fixtures cover weather, faults, planned restrictions, delays, suspensions, cancellations, partial/full restoration and strikes; unrelated-region, freight and standing-page exclusions remain tested.
- [ ] Bus fixtures cover each operator's notices: closed or relocated stops, diversions, suspended services, timetable changes, ATAP-only exclusion and a repeated notice.
- [ ] Possible impact is never rendered as a confirmed individual cancellation. Missing fields remain unknown.
- [ ] First poll publishes active/future notices but not terminal/archive notices. Meaningful edits emit; identical, formatting-only and timestamp-only edits do not.
- [ ] Latest-status interpretation handles accumulated bulletin histories, partial restoration, explicit withdrawal, disappearance, expiry and renewed disruption correctly.
- [ ] Identity survives title/date/impact edits; A → B → A revisions have distinct GUIDs.
- [ ] An existing rail database upgrades atomically: unchanged notices do not replay, newly covered disruptions publish, a restart does not duplicate, and a failed migration preserves old state and history.
- [ ] Unsupported version transitions and SeaDex initialization/rebaselining retain existing behavior.
- [ ] A missing page, a page without its collection structure, or a malformed response fails the poll and preserves the stored baseline.
- [ ] Temporary-SQLite polling through both RSS and HTML endpoints proves persistence, escaping and safe links.
- [ ] Opt-in live probes on the deployment host report parsed coverage per required collection/operator without touching the normal database; a legitimate empty collection is distinguished from unverified coverage or parser failure.

Run focused Flow and engine regression tests first, then `go test ./...` for cross-cutting changes. Run live probes separately from deterministic CI. Record commands and outcomes against this ledger; compilation alone is insufficient.

## Non-goals

Journey planning, individual departure tracking, guarantees of service availability, every regional transport operator, ATAP-only coverage, unofficial news/union aggregation, push notifications and reconstructing updates missed between polls.
## Implementation notes

### Shipped

- `trenitalia-disruptions` publishes every operator-confirmed disruption that
  reaches FVG, not only strikes: the captured 10 September weather bulletin on
  the Venezia - Trieste line and the region's `INFOLAVORI` works page are
  extracted from the existing fixtures, and a live probe on the deployment host
  reports the same two notices against the current page.
- Eligibility is an operational impact plus covered-service applicability.
  Scope still comes from the FVG region tag, a covered route, or a national
  statement that includes the region. A standing page carries several regions in
  its body, so only its title establishes coverage; that is what keeps the
  national high-speed delay list and the regional information index out of the
  feed.
- A title stating that circulation is regular again is stored as `Ripristinato`
  and announced only for a disruption that was already tracked, so a first
  observation never reports history as news. `Revocato` keeps its meaning.
- `Source.EmitVersionChanges` lets an opted-in Source compare its stored
  baseline across an extraction-version change. An existing strike-only
  database therefore receives the newly covered disruptions without replaying
  unchanged strikes, and a failed poll still preserves baseline and history.
- `internal/sources/trenitalia/testdata/notizie_fvg_restored.html` is a
  derived fixture (see the fixture notes) because upstream had already restored
  the captured bulletin.

### Bus coverage

TPL FVG's own alert hub is `https://tplfvg.it/it/infomobilita/avvisi-sul-servizio/`,
a prose page that links to the operators rather than a notice collection, so the
bus notices are read where the operators publish them:

| Feed | Collection | Rule |
| --- | --- | --- |
| `arriva-udine` | the operator's notice endpoint | the endpoint also serves the whole archive, so the feed reads a fixed newest window and fails the poll on a response without notices |
| `trieste-trasporti` | `triestetrasporti.it/it/avvisi-infomobilita` | only the cards above the `#archivio-avvisi` marker are in force, and a page without the marker fails the poll instead of republishing expired notices |
| `apt-gorizia` | `aptgorizia.it/in-evidenza/modifiche-al-servizio-riepilogo-aggiornato-...` | only the notices and route diversions the summary of changes in force links |

One Source per operator, rather than one bus feed over three origins: the
project's model is one upstream resource per feed, and three polls fail
independently. The shared decoding, identity and change semantics live in
`internal/sources/notices`. See `docs/adr/0010-per-operator-bus-feeds.md`.

### Still open

- The polling interval question: the specification proposed ten minutes, and
  every feed still uses the default hourly interval.
- Arriva Udine summaries: its endpoint leaves the notice text on the page, so
  the feed carries the title, the date and the permalink. A detail fetch would
  extend that Source alone.
- Rail variants beyond the captured fixtures: a planned-works-only month, a
  partially restored line, and an explicit withdrawal of a non-strike notice.
