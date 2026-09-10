# Bus notice dates, stated windows and a freshness window

Amends ADR 0010 and the bus rules of `docs/tech-spec-fvg-disruption-feeds.md`.
The rail feed keeps its behaviour.

The bus feeds published every notice their page listed and dated every feed
entry with the time rfs observed it. Two things went wrong with that:

- A notice the operator had been showing for months — an Easter timetable, a
  road closure, a stop suspension from 2022 — reached a new subscriber looking
  exactly like today's news, because nothing said when the operator published it
  or when it applied.
- The opposite mistake is just as easy: an August road closure is not over
  because it is a month old. Publication age and expiry are different facts.

## Decision

### Dates are structured and optional

`notices.Notice` carries `Published Date` and `Validity{Start, End Date}` in
place of one free-text `Date`. A `Date` keeps the wording the page used
(`Text`), the instant it decoded to in `Europe/Rome`, and whether the wording
named a calendar day or a clock time. A bound that the page does not state stays
zero, and an unreadable bound keeps its wording and no instant. Unknown is a
value, not a default.

Operator packages decode their own page into those fields; the shared `notices`
package owns what they mean. `notices.ValidityFromText` reads a bounded grammar:
explicit ranges, open-ended starts, stated ends and single-day events, with
Italian month names, numeric and ISO dates, and an optional weekday or clock
time inside the phrase. A bare date states nothing: `aggiornamento del
16-06-2026` and `processione il 08/09/2026` are not windows. A missing year is
never filled from the clock; `dal 6 luglio` is shown as published and states no
bound.

### A notice is announced only while it is still live

`notices.Eligible` decides, and it is applied to emissions rather than to the
baseline:

- A stated window that has clearly ended suppresses the notice. A day bound
  covers that whole local day, so a strike announced for 10 September is still
  live at 14:00 on 10 September and over on the 11th.
- A notice whose own publication date is older than two calendar months is
  suppressed as no longer news. The cut is a calendar subtraction in
  `Europe/Rome`, clamped to the end of a short month.
- Missing, ambiguous, contradictory or future dates suppress nothing. A start
  date is not an age, so APT Gorizia's `dal 7 febbraio 2022` notices survive the
  cutoff, and an observation time is not a publication date either.
- The cutoff is a freshness threshold, not a claim that a disruption has ended.

Every observed notice stays in the comparison baseline, announced or not. A
suppressed notice is therefore never re-discovered as new on the second poll,
and a notice whose operator extends its window reports a real update.

### The comparison takes the poll instant

The decision depends on time, so `notices.Flow` implements `rfs.ClockedChangeFlow`
and the engine hands it `Poller.Clock`'s instant. `ClockedChangeFlow` embeds
`Flow`, not `ChangeFlow`: a Flow whose decision needs an instant should not also
have to offer a comparison without one. Existing `ChangeFlow`s are untouched.

### What readers see

Descriptions separate `Pubblicato` (the notice's own date) from `Validità` (the
stated window) and, when the entry timestamp is the observation time,
`Rilevato`/`Aggiornamento rilevato`. A new entry carries the notice's own
publication time as its feed timestamp when the page stated a reliable,
non-future time; a day-only or missing date stays in the description and the
entry keeps the observation time. Updates always keep the observation time, so
an edit is not backdated.

## Consequences

- Two deliberate bus-only deviations from the rail rules in
  `tech-spec-fvg-disruption-feeds.md`: "RSS dates remain observation times" and
  "do not use the notice publication date as an expiry date". The bus feeds
  publish a reliable publication time as the entry timestamp, and they do use
  publication age to stop publishing notices that are no longer news. Both
  concern what a subscriber is shown, not a declaration that a disruption is
  over.
- Trieste Trasporti's card timestamp is treated as the notice's own date. The
  page already separates the cards in force from `#archivio-avvisi`, and the
  parser fails without that marker, so the date is a date the operator shows,
  not an expiry claim. The theme writes it as a wall-clock reading with a `Z`
  suffix; the calendar day is taken as written.
- APT Gorizia states no publication date anywhere, so the cutoff never applies
  to it and no proxy is invented for it. Its summary page listing only the
  changes in force remains the mechanism.
- A notice that leaves the page still emits nothing: rfs does not report a
  cancellation or a restoration it cannot prove.
- Extraction version 2 changes the payload shape. A stored version-1 baseline is
  rebaselined in silence and is never decoded with the new decoder, so an
  upgrade neither replays the archive nor fails the poll.
- Readers keep entries they were already sent: expiry is not retraction, and
  the feed is not a live departure board. What changes is the served feed — the
  handler asks the Flow at render time (`rfs.LiveFlow`) and stops offering a
  stored entry once its window ends or its publication leaves the freshness
  window, so a subscriber never receives one for the first time. The rail and
  catalog feeds do not implement that contract and keep their full history.
- Not implemented: fetching a notice's own page to recover validity dates the
  collection omits. It would add one request per notice, so it is out of scope
  until an operator page makes it necessary.

## Rejected alternatives

- A fixed 60-day window: the request was calendar months, and clamping matters
  at month ends.
- Inferring expiry from publication age alone: that is exactly the mistake this
  ADR separates out. Age is a freshness rule with its own, narrower effect.
- Computing Easter, or any holiday, from a title: not evidence.
- Treating a bare date as a one-day event: it would silently expire notices that
  merely mention a date.
- A silent first-observation baseline for the bus feeds: subscribers of a new
  feed should still see what is in force, which is why the cutoff filters
  emissions instead of hiding the first poll.
- Reading validity dates from a linked detail page: extra requests and a new
  failure mode for data the operator does not always publish.
