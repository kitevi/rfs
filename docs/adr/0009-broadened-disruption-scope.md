# Operator-confirmed transport disruption feeds

This is the current decision record for all four transport feeds. Source IDs,
feed URLs and announcement-history semantics remain stable.

## Sources and applicability

Each Source has its own poll and failure boundary:

| Source | Collection | Coverage |
| --- | --- | --- |
| `arriva-udine` | WordPress notice endpoint, newest window | Arriva Udine services |
| `trieste-trasporti` | Active notice cards, before the archive marker | Trieste Trasporti services |
| `apt-gorizia` | Linked active notices and diversions | Gorizia and Monfalcone services |
| `trenitalia-disruptions` | Infomobilità collection | Trenitalia passenger services affecting FVG |

An official operator statement must establish both applicability and operational
impact. Strikes, weather and technical incidents, suspensions, cancellations,
delays, diversions, replacement buses, timetable changes, stop closures and
planned works qualify. ATAP-only notices, unrelated regions and freight-only
incidents do not. Genuine strike terminology in upstream URLs and classifiers is
part of the current domain, not obsolete naming.

Rail applicability includes the FVG region tag, routes serving Udine, Trieste,
Gorizia or Monfalcone, and applicable national operator notices. Standing pages
must establish coverage in their own title; a passing body reference is not
enough. Explicit terminal titles are not announced. Disappearance emits nothing;
only an explicit operator revocation establishes revocation.

## Shared bus flow

Operator parsers return `notices.Notice` values. The shared Flow validates stable
permalink identities, compares observations, and emits additions or edits.
Every observed notice stays in the comparison baseline, including ineligible
ones. Disappearance does not imply that the service resumed.

Dates retain original wording, decoded Europe/Rome time, and day or minute
precision. Arriva supplies minute-precision WordPress wall-clock timestamps.
Trieste supplies calendar days, taken as written despite the theme's Z suffix.
APT's collection supplies no publication date; validity is not a substitute.
Validity parsing uses only explicit wording in the fetched collection, not
holiday calculations or extra detail-page requests.

A stated end excludes a notice after that instant, or after the entire stated
calendar day for day precision. Publication older than two calendar months also
excludes it, with month-end clamping. That freshness cutoff is not evidence that
the disruption has ended. Unknown dates do not establish either expiry or age.
The same predicate applies during emission and at request time for HTML and RSS.

## Storage and presentation

Extraction JSON belongs to the comparison baseline. Published descriptions are
human-readable text. Published bus items carry separate versioned metadata with
the notice's publication, validity, identity and observation timestamp. Metadata
passes through announcement creation, the poller and SQLite; descriptions are
never parsed to recover machine state.

HTML uses an explicit publication label, including only the precision known.
Unrecoverable publication is labelled unavailable. New RSS announcements use a
non-future minute-precision publication timestamp when known; otherwise they use
observation time. Edit announcements retain observation timestamps. Dates and
validity remain explicit in descriptions. The renderer does not fabricate a
midnight instant for a calendar day.

Missing or unsupported metadata establishes neither a publication date nor
expiry. There is no legacy description decoder, enrichment fetch, repair table,
or write-on-read path. A database containing pre-metadata bus items requires a
separate operator-managed repair or bus-only reset before deployment; back up
first. Clearing validators alone cannot rewrite existing announcements.

## History and versions

First observations publish eligible notices. Additions and edits retain
revision-qualified RSS GUIDs, including A → B → A changes. Bus request-time
filtering does not delete stored history or retract a reader's cached items.
Rail does not implement bus liveness filtering and retains its served history.

Extraction versions invalidate cached validators when code changes derivation.
Bus feeds silently rebaseline on version mismatch. Rail opts into
`EmitVersionChanges` to compare a changed derivation against its baseline.
These generic engine contracts remain; no operator-specific upgrade shim is
needed. Other sources retain their existing policies.

## Verification

Fixtures cover each operator's parsing and scope. Integration tests exercise real
Extract → ChangesAt → poller → SQLite → HTTP announcements, database reopen,
metadata round trips, date labels, unchanged GUIDs and clock-only age-out.
Unknown, malformed, day-only and future publication values have focused tests.
Rail classification, terminal states and change-history tests remain in place.

Run `go test ./...` and `go vet ./...`. Production deployment and any one-time
state maintenance are separate operations; verify the deployed build and both
feed formats after release.
