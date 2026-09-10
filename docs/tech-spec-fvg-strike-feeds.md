# FVG operator-confirmed strike feeds

Status: proposed; upstream extraction verification required before implementation.

## Goal

Add exactly two Sources, each with one Flow and one RSS feed:

| Source ID | Coverage | Feed |
| --- | --- | --- |
| `tpl-fvg-scioperi` | Arriva Udine, Trieste Trasporti, APT Gorizia including Monfalcone | `/feeds/tpl-fvg-scioperi.xml` |
| `trenitalia-scioperi` | Trenitalia passenger services affecting travel in Friuli Venezia Giulia | `/feeds/trenitalia-scioperi.xml` |

The existing router also provides the corresponding `.html` views. RSS readers provide notifications; rfs does not send push messages.

Only publish operator notices establishing applicability to the covered services. Do not add MIT, union, news-aggregation, or separate national-warning feeds.

## Meaning of confirmation

An operator notice stating that covered services may be disrupted by a specified strike qualifies. Actual employee participation and individual cancellations need not be known in advance. The word “proclamato” does not itself disqualify an operator notice: examine its service applicability, not a keyword blacklist.

Do not publish a generic proclamation that fails to establish applicability. Do not describe a strike as an individual train cancellation. Use source-faithful language such as “Servizi potenzialmente interessati” when the operator describes potential disruption. Only label a notice revoked when the operator explicitly says so.

## Current architecture

- `internal/sources/sources.go` registers hardcoded Sources.
- `internal/rfs/types.go` defines pure `Flow.Extract(Page)` and `Version()`.
- `internal/rfs/changes.go` defines pure `ChangeFlow.Changes(previous, current)`.
- Change feeds persist complete baselines and emitted items atomically in SQLite. The engine supplies observation-time dates and revision-qualified GUIDs, including repeated transitions such as A → B → A.
- First observations and extraction-version changes currently establish silent baselines.
- `EnrichedChangeFlow` performs metadata fetching after comparison. It is not sufficient for fetching notice bodies needed to determine identity, applicability, or changes.

Use `ChangeFlow`, not catalog `HistoryPolicy`. Keep existing Sources unchanged.

## Upstream discovery gate

Before implementing selectors, fetch the actual resources from the deployment environment and save representative fixtures. Search-engine extracts are discovery evidence, not proof of current availability, completeness, or DOM structure.

Bus candidate, documented by TPL FVG as its strike-notice page:

`https://tplfvg.it/it/servizi/scioperi/`

Rail candidate, not yet verified as a complete strike collection:

`https://www.trenitalia.com/it/informazioni/Infomobilita/notizie-infomobilita.html`

For each candidate, establish:

1. Whether the fetched representation contains full notices or only links; whether JavaScript or pagination hides relevant notices.
2. Stable upstream notice IDs or canonical permalinks.
3. How active notices, archives, explicit revocations, and an empty active collection are represented.
4. Whether edits to a detail notice change the collection representation or its HTTP validators.
5. Whether all active relevant notices remain discoverable, including notices older than the first page.
6. Representative applicable, unrelated, updated, revoked, and empty states.

For rail, verify national and FVG notices, not just real-time delay bulletins. If this candidate cannot reliably expose operator strike notices, identify an official Trenitalia collection before coding the parser. Do not silently substitute MIT or claim rail coverage is complete.

If detail fetches are required, add a small engine-owned collection/detail fetching contract in a follow-up ADR before implementation. It must refresh active details even when the index returns 304, use the shared fetcher/domain gate, constrain requests to verified official HTTPS origins, and assemble a complete observation before comparison. Failed required requests must prevent the entire poll from committing. Do not perform HTTP in a Flow or force detail fetching into post-comparison enrichment. This is a conditional design task, not an existing capability.

Report access blocks; do not bypass them. A source that cannot be verified is not ready to register as working.

## Flow 1: TPL FVG buses

Package: `internal/sources/tplfvg`.

Include notices applying to Arriva Udine, Trieste Trasporti, or APT Gorizia; include consortium-wide notices when their scope includes these operators. Exclude ATAP-only notices and unrelated transport modes.

Extract active notice blocks and explicit revocations, excluding the historical archive and standing guarantee text as standalone items. Generic guarantee windows are context, not evidence of an upcoming strike. Prefer event-specific instructions over standing windows.

Fields: stable notice identity, canonical link, operator/service scope, strike date and time text, explicit status, guarantee instructions, relevant notice body, and upstream publication/update date when supplied. Preserve the original Italian wording. Missing hours must remain unknown, not inferred.

Examples of titles (illustrative, not current announcements):

- `[Bus · Udine] Sciopero — <data e orari>`
- `[Aggiornato · Bus] Arriva Udine — <data>`
- `[Revocato · Bus] APT Gorizia — <data>`

## Flow 2: Trenitalia trains

Package: `internal/sources/trenitaliascioperi`.

Include operator notices explicitly affecting:

- Trenitalia regional passenger services in FVG;
- routes or services serving Udine, Trieste, Gorizia, or Monfalcone;
- national passenger services with scope encompassing FVG, even when individual FVG cities are not named.

Exclude notices restricted to unrelated regions, freight-only activities, and personnel categories that the notice does not connect to covered passenger services. Do not equate every national railway-sector proclamation with applicability. Ambiguous notices must not become confident claims of local disruption; test and document classification rules against real fixtures.

Extract notice identity, canonical link, service/personnel scope, strike interval, explicit status, guarantee and refund instructions, relevant body, and official supporting links. Do not hardcode guarantee windows or claim that trains absent from the guaranteed list are cancelled. This Flow does not monitor individual train running status.

Examples:

- `[Treni · FVG] Sciopero — <data e orari>`
- `[Treni · Nazionale] Sciopero con impatto sui servizi FVG — <data>`
- `[Aggiornato · Treni] Modificate le indicazioni sui servizi garantiti`

## Observation and identity model

Each extracted item represents one upstream notice, not one keyword match or one city. Multiple covered operators in one notice produce one item. Deduplicate desktop/mobile renderings by upstream identity.

Use an upstream ID or canonical notice permalink as entity GUID. Do not include mutable dates, hours, status, body hashes, or list position in identity. If no stable identity exists, define and fixture-test an explicit source-specific strategy during discovery; do not rely on fuzzy merging of separate strikes.

Store a versioned structured comparison payload in `ExtractedItem.Description`, following the existing change-feed pattern. Render human-facing descriptions only from `Changes` or initial emission. Compare normalized service scope, strike interval, explicit status, guarantees, instructions, and relevant body. Preserve meaningful wording while ignoring navigation, analytics parameters, whitespace, and update timestamps alone.

Notifications:

| Transition | Result |
| --- | --- |
| New qualifying notice | New announcement |
| Meaningful edit | New item summarizing changed fields |
| Explicit revocation | Revocation item |
| Same notice/content | Nothing |
| Formatting-only change | Nothing |
| Notice disappears or expires | No inferred cancellation |
| Invalid/incomplete page | Fail poll; preserve baseline and feed |
| Extraction-version change | Silent rebaseline; preserve history |

For a notice returning after absence, describe it as newly observed again, not a newly proclaimed strike. Revision-qualified engine GUIDs allow readers to detect the new observation.

An empty observation requires a recognized valid empty state or a validated complete collection with no qualifying notices. A login page, bot challenge, missing expected section, or malformed structure is not an empty collection.

Use plain-text descriptions initially, leaving `ItemDescriptionsHTML` false. Include what changed, operator/service scope, dates/hours as stated, guarantee instructions, and the source link. Validate emitted links as HTTP(S); never render untrusted upstream HTML.

## First-run behavior

Proposed default for these two Sources: publish notices currently presented as active by the verified upstream collection, then emit only changes. This avoids an empty feed while an upcoming strike is already known.

Introduce an optional interface in `internal/rfs/changes.go`:

```go
type InitialChangeFlow interface {
    ChangeFlow
    InitialChanges(current []ExtractedItem) ([]ExtractedItem, error)
}
```

On `!previous.Initialized`, invoke this interface when implemented; otherwise retain the existing silent baseline. Feed initial items through the same enrichment, revision, and atomic persistence path as ordinary changes. On a version mismatch for an initialized source, retain silent rebaselining regardless of this interface.

Do not call `Changes(nil, current)` implicitly for every existing change feed. SeaDex must remain silent on first poll. If initial publication fails, do not mark the baseline initialized.

Extraction stays deterministic for fixed input: no `time.Now()` filtering inside Flows. Prefer an upstream active collection. If a source mixes undifferentiated archived and active notices and cannot provide reliable activity markers, resolve that explicitly before implementation rather than guessing dates or expanding scope unnoticed.

## Polling and retention

Use the existing default hourly poll interval, adjustable with `-interval`; no new CLI flags. Respect shared conditional fetching, throttling, and domain spacing. Refresh detail bodies independently when required by the verified upstream design.

RSS dates are observation times; strike dates belong in titles/descriptions. Existing persisted change history has no automatic pruning. This proposal does not change that policy. Notifications reflect observed changes only; changes made and reverted between polls can be missed. No countdown reminders or guaranteed instant alerts.

## Planned changes

- `internal/sources/tplfvg/flow.go`, `flow_test.go`, `testdata/`, optional opt-in `live_test.go`.
- `internal/sources/trenitaliascioperi/flow.go`, `flow_test.go`, `testdata/`, optional opt-in `live_test.go`.
- `internal/sources/sources.go`: register exactly the two IDs above with official human-facing links and plain-text metadata.
- `internal/sources/sources_test.go`: verify registrations, uniqueness, metadata, and Flow contracts.
- `internal/rfs/changes.go` and targeted engine tests: optional initial emission.
- `docs/adr/0009-operator-confirmed-strike-feeds.md`: record confirmation scope and initialization amendment to ADR 0008.
- `README.md`: document both RSS/HTML URLs, scope, first-run behavior, and the distinction between service applicability and individual cancellations.
- Conditional collection/detail fetching files only if discovery proves they are required; document the precise contract first.

No implementation changes are made by this specification.

## Acceptance ledger

1. Exactly two new Sources and feed pairs exist; no MIT Source or third transport feed.
2. Bus fixtures cover each target operator, consortium-wide scope, ATAP-only exclusion, archives, and explicit empty state.
3. Rail fixtures cover national applicable notices without city keywords, FVG notices, covered routes, unrelated-region exclusion, and non-passenger exclusions.
4. Official notices using “proclamato” still qualify when applicability is established; unsupported proclamations do not.
5. Date/hour changes retain entity identity and emit a fresh revision GUID; unchanged and formatting-only polls emit nothing.
6. Explicit revocation emits an item; disappearance or expiry never claims cancellation.
7. First run publishes active notices; restart does not duplicate them; SeaDex remains silent initially.
8. Version bumps silently rebaseline; A → B → A generates distinct revision notifications.
9. Malformed pages, HTTP failures, blocked responses, and incomplete detail collections preserve both baseline and emitted items.
10. Feed descriptions cannot inject HTML or unsafe links; RSS and HTML endpoints render successfully.
11. Live probes demonstrate real upstream extraction on the deployment host without mutating the normal database. Missing live coverage is reported, not represented as a passing check.

## Verification and delivery order

1. Finish source discovery and capture fixtures; resolve the rail collection and any detail-fetch requirement.
2. Add fixture-driven extraction/classification tests and implement the two pure Flows.
3. Add optional first-run emission with targeted engine persistence/regression tests.
4. Register Sources and verify RSS/HTML behavior against fixture-fed polling using a temporary SQLite database.
5. Run targeted package tests, then `go test ./...` for the cross-cutting engine change.
6. Run opt-in live extraction probes separately from deterministic CI. Confirm both transport feeds directly, not only compilation.
7. Update the README and ADR with verified behavior and remaining upstream limitations.

Out of scope: push delivery, MIT monitoring, union/news feeds, individual train status, journey planning, calendar reminders, and guarantees that a specific departure will operate.
