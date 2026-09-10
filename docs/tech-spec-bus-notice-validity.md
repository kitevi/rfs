# Bus notice validity and date presentation

Status: implemented. The decision and its consequences are recorded in
`docs/adr/0011-bus-notice-dates-and-expiry.md`; what follows is the
implementation the ledger at the end was checked against.

## Goal

Exclude explicitly expired bus notices from new feed emissions and display the
operator's dates accurately, using only the response already fetched per poll.
Also suppress new emissions for notices published more than two calendar months
ago. This freshness policy does not mean that those disruptions have expired.

This specification refines `tech-spec-fvg-disruption-feeds.md` and ADR 0010 for
Arriva Udine, Trieste Trasporti and APT Gorizia. Rail eligibility, SeaDex,
pagination, source IDs and feed URLs remain unchanged.

## Verified current behavior

- `internal/sources/notices/flow.go` stores a single free-text `Date` and compares
  notice content by permalink identity. Disappearance emits nothing.
- The bus Sources enable `EmitInitial`: their first observation emits notices.
- `internal/rfs/changes.go` assigns observation time to every emitted item's
  `PubDate`, regardless of the upstream date.
- Arriva reads the ten newest WordPress notices, not an authoritative active-only
  collection. Its response includes `date`, title and sometimes content. The
  captured Easter notice has `date=2026-03-30T13:08:59`, empty content and no
  explicit validity interval. There is no verified `new` flag involved.
- Trieste's parser reads a card's `time[datetime]` into its date label. That
  timestamp is not, by itself, proof of a validity end.
- APT currently extracts links and titles; it does not populate `Notice.Date`.
  Nearby validity text must be verified in the fetched summary before extracting
  it. Dates visible only on a linked detail page are unavailable under this scope.

Existing tests passing does not establish correct expiry behavior. Implementation
must first produce a deterministic failing test for a clearly expired notice.

## Policy

1. Keep future notices, currently valid notices and open-ended disruptions,
   subject to the publication-age cutoff below.
2. Suppress emissions for notices with a confidently established validity end
   before the evaluation instant. An end date without a time includes that whole
   local calendar day.
3. Never infer expiry from publication age, list position, or a start date alone.
4. Missing, ambiguous or contradictory validity information means unknown, not
   expired. Preserve readable upstream wording; apply the publication-age cutoff
   independently even when validity is unknown.
5. A notice disappearing or expiring does not imply cancellation or restoration;
   emit no invented event.
6. Explicit changes extending or reopening validity can make a notice eligible
   again only if it also passes the publication-age cutoff. Ordinary edits to a
   still-expired notice must not publish it.

### Two-month publication-age cutoff

Suppress a candidate emission when its reliable upstream publication date is
strictly earlier than the poll's local calendar date minus two calendar months
in `Europe/Rome`. This is not a fixed 60-day duration. Clamp the day to the last
valid day of the target month when necessary. The boundary date is included:
on September 10, 2026, July 10 passes and July 9 does not. Compare local dates,
not time-of-day, for both date-only and timestamp publication values.

Apply this rule on initial and subsequent polls, including update emissions.
It also applies to ongoing, open-ended and future disruptions: this is an explicit
freshness trade-off, separate from expiry. Extending validity or observing an edit
does not reset publication age. A changed upstream publication date is evaluated
as supplied, but observation time and modification time are not substitutes.

If publication is missing, invalid, future-dated or semantically uncertain, do not
apply the age cutoff; still apply explicit expiry rules. In particular, APT's
“dal 7 febbraio 2022” alone cannot trigger this cutoff because it is a validity
start. Do not add requests to discover a missing publication date.

No archive purge or silent initial baseline is introduced. Already-emitted items
remain in stored history and readers; the cutoff only controls new emissions.

## Date model

Replace the overloaded bus `Date` field with structured, optional metadata:

- Publication: upstream publication date/time, raw text and precision.
- Validity: optional start and end, raw text, and parsing provenance.
- Precision: calendar date versus timestamp; unknown is an explicit absence.
- Provenance: the field or matched phrase supporting each interpretation, including
  any defensible year inheritance. It is not a fabricated confidence score.

Use `Europe/Rome` for operator-local dates and timezone-less WordPress timestamps.
Honor explicit offsets. Date-only values remain date-only in user-facing text.
Do not fill missing years from the current clock. A year may be inherited from an
explicitly associated date in the same interval; otherwise leave it unresolved
unless a source-specific, fixture-backed rule establishes the year unambiguously.
Publication year alone is not a universal validity-year rule.

Publication, validity and observation timestamps are distinct concepts. None may
be substituted for another in eligibility tests.

## Extraction, without additional requests

Operator packages decode available metadata; shared `notices` code owns normalized
validity interpretation, eligibility and presentation. Extraction remains pure.

- Arriva: parse the WordPress publication timestamp. Inspect the already-returned
  title and content for explicit event days or validity intervals.
- Trieste: preserve the card timestamp as the upstream notice date; verify its
  semantics before labeling it publication. Parse validity only from distinct,
  explicit wording in the same card.
- APT: inspect the notice's title and associated summary text. Do not accidentally
  attach a neighboring notice's dates to it. `dal 7 febbraio 2022` is a start, not
  an end or a publication date.

Support a bounded, tested grammar rather than a general natural-language date
parser: Italian month names, explicit single-day events, explicit ranges and
open-ended starts. A bare date is not automatically a one-day event. A phrase
such as a strike “per il giorno 19 giugno 2026” establishes a bounded event;
“variazioni dal 13 aprile 2026” does not establish an end.

Multiple validity intervals expire only when all relevant intervals have known
ends and all have elapsed. Mixed unknown/open intervals prevent an expired verdict.
Invalid calendar dates and unrelated dates in prose must not produce an expiry.

Do not calculate Easter from a title or treat a holiday name as an explicit end.
The captured Easter notice has unknown expiry but is suppressed on September 10,
2026 by the publication-age cutoff: its March 30 publication precedes July 10.
This requires neither a guessed Easter interval nor an additional request.

## Baseline and clock integration

Store the complete decoded observation, including expired and over-age notices and structured
dates. Filter candidate emissions, not baseline membership. This prevents an
expired notice from being rediscovered as new on the second poll, while preserving
comparison data for an explicit reopening or extension.

Expiry and publication age are evaluated at a single poll instant supplied by the engine's injectable
clock. Do not call `time.Now()` from parsers or add a clock-dependent field to the
serialized baseline. Implement an optional clock-aware change-flow hook if the
current interface cannot carry this instant; existing ChangeFlows retain their
current behavior. Its exact signature is an implementation decision, covered by
engine tests.

Initial emissions and later changes use the same eligibility predicate. Crossing
an end or publication-age boundary emits nothing and need not mutate the baseline. A 304 response
therefore needs no forced fetch or artificial change just to mark expiry. The next
changed response evaluates candidates against the current poll instant.

Already-published snapshots remain historical entries, not a live active-status
view. This design does not promise to retract expired items from RSS readers.

## Presentation and feed timestamps

Bus descriptions distinguish `Pubblicato`, `Validità` and `Rilevato/Aggiornamento
rilevato`. If the source timestamp's meaning is uncertain, use `Data avviso`
instead of asserting that it is publication time. Omit unknown fields.

For an initial or newly discovered notice, use a reliable, non-future publication
timestamp as RSS/HTML `PubDate`. Never use a validity start or end as `PubDate`.
For date-only publication information, display the actual date in the description
but retain observation time as `PubDate`, explicitly labeled as discovery time.
Missing, ambiguous or future publication timestamps also fall back to observation
time. This avoids inventing time-of-day precision or future-dating feed events.

For a meaningful update to an existing notice, retain observation time as the
item timestamp so the update is not backdated. Include the original publication
and validity dates in the update description.

Use the existing optional `ExtractedItem.PubDate` as the emitted timestamp override
where appropriate; extend engine handling with an explicit opt-in if needed so
unrelated ChangeFlows retain their current semantics. Test both RSS and HTML,
including their ordering behavior when notices have older publication dates.

## Upgrade and persistence

Bump the bus extraction version and version the changed notice payload. Bus Sources
currently use silent version rebaselining rather than `EmitVersionChanges`; keep
that behavior for this metadata correction. Verify the engine does not decode old
payloads with the new decoder during rebaselining.

Rebaseline atomically without replaying the list, rewriting historical GUIDs or
republishing old items solely to correct their dates. A failed fetch, decode or
transaction must preserve baseline, validators and snapshots. Existing stored
entries keep their old timestamps/descriptions; new emissions follow this spec.
An optional historical repair would be a separate, explicitly authorized task.

## Implementation record

Where the ledger asks for a decision rather than a check, the decision is named
with the test that holds it. Every fixture used is a capture, not a scenario:
see the `testdata/NOTES.md` in each operator package.

| Ledger item | Where it is held |
| --- | --- |
| A real explicitly dated Arriva event: live on its own day, gone the next | `TestBusFeedsAnnounceOnlyWhatIsStillLive` polls the captured page on 10 and 11 September; the 4-hour strike for 10 September is announced on the 10th and, from a fresh store on the 11th, is not |
| Easter suppresses on 30 March despite unknown expiry | same test asserts the March notice is absent and present in the baseline |
| July 10 passes, July 9 fails, clamping, leap years, Rome day | `TestCutoffStepsBackTwoCalendarMonths`, `TestCutoffUsesTheRomeCalendarDayOfTheObservation`, `TestEligibleKeepsNoticesTheCollectionDoesNotExpire` |
| Over-age suppression applies to updates too | `Eligible` gates every change, not only first sightings: `TestChangesAtIgnoresEditsToEndedNoticesButAnnouncesExtensions` |
| Unknown publication is never a proxy | `TestParseNoticesKeepsTheActiveNotices` (APT states none, none is invented) and `TestBusFeedsKeepNoticesTheCollectionCannotDate` |
| An August Trieste notice is not expired | `TestBusFeedsKeepNoticesTheCollectionCannotDate` |
| Ranges, open starts, same-day ends, future events | `TestValidityFromTextReadsTheStatedWindow`, `TestEligibleKeepsNoticesTheCollectionDoesNotExpire` |
| Ambiguous years, bare and unrelated dates | same grammar table plus `TestChangesAtUsesThePublicationTimeOnlyWhenItNamesOne` |
| Extension emits one update with stable identity | `TestChangesAtIgnoresEditsToEndedNoticesButAnnouncesExtensions` |
| Publication overrides survive persistence and render | `TestBusFeedsAnnounceOnlyWhatIsStillLive`, `TestPollerComparesAtTheClockInstantAndHonorsExtractedPubDate` |
| Upgrade rebaselines without replay | `TestBusFeedUpgradeRebaselinesWithoutReplayingTheArchive` |
| 304, SeaDex and rail unchanged | the existing engine, SeaDex and rail tests, unchanged and passing |
| One request per bus poll | the fixtures' fetcher fails on any URL the Source does not declare |

### Decisions taken during implementation

- `rfs.ClockedChangeFlow` embeds `Flow`, not `ChangeFlow`. A Flow whose
decision needs the poll instant must not also have to offer a comparison
without one, so `notices.Flow` implements only `ChangesAt` and the engine
dispatches on either contract.
- The engine now honours `ExtractedItem.PubDate` on change-feed emissions. It
already did so on the projection path, so this removes an inconsistency rather
than adding a special case.
- `time/tzdata` is embedded in `internal/sources/notices`, so `Europe/Rome` is
available without relying on the host's zoneinfo.
- Trieste Trasporti's card timestamp is treated as the notice's own date at day
precision, and its afternoon-of-the-capture values are taken as written rather
than shifted by the `Z` suffix the theme adds.
- A start whose year is not stated stays undecoded and is shown as the operator
wrote it. It neither expires nor dates the notice.
- No synthetic fixture was needed: every behaviour above is exercised against a
captured response, including the one case the rail feed would call a historical
alert.
- `git grep` finds no remaining free-text notice date: `Notice.Date` is gone, and
the extraction and payload versions are 2.
