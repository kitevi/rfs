# Operator-confirmed strike feeds with detail observations

Amends ADR 0008: a change feed can now require secondary pages before it can
observe anything, and a Source can publish its first complete observation
instead of starting silent. Adds the two Sources specified by
`docs/tech-spec-fvg-strike-feeds.md`.

## Confirmation scope

A notice qualifies when the operator states that covered services may be
disrupted by a specified strike. Pending strikes with unknown participation
qualify; individual train or trip cancellations are never claimed, and a missing
strike window stays unknown rather than inferred. "Proclamato" is not a
disqualifier — applicability, not vocabulary, decides. A generic proclamation
that does not establish applicability to covered services is not published.
Only an explicit operator statement labels a notice revoked, and a notice that
disappears or expires produces no item: disappearance is not cancellation.

## Detail observations

`DetailFlow` extends `Flow` with `DetailURLs(Page)` and
`ExtractDetails(Page, []Page)`. rfs, not the Flow, performs the IO:

- Declared links are resolved against the Source URL and must stay on the
  Source's HTTPS origin. Duplicates and more than 100 requests are rejected
  before any request is made.
- Every required page is fetched with the shared fetcher inside the poll's
  domain-gate lane, so spacing and Retry-After apply as for a primary fetch.
- A missing, throttled, not-modified or unparsable required page fails the poll
  before comparison, so the stored baseline and already emitted items survive.
  A partial observation never replaces a complete one.
- Detail sources always fetch their collection unconditionally, and a 304 for
  the collection is an error rather than "unchanged": a detail body can change
  while the collection bytes stay the same. This extends ADR 0008's page-one
  validator rule to detail pages.

## Initial emission

`Source.EmitInitial` publishes the first complete observation through
`Changes(nil, current)`, so a new subscriber sees the notices already in force.
An extraction-version mismatch still rebaselines silently, and the zero value
keeps ADR 0008's silent baseline (SeaDex).

## Sources

`tpl-fvg-scioperi` watches TPL FVG's strike page, which lists each notice with a
validity badge; cards for unrelated services share the same markup, so the
notice permalink decides what is a notice. Applicability lives only on each
notice's page, which is why this Source needs detail observations. The Flow
reads the operator's applicability summary, never treats an operator named as
running a regular service as affected, expands consortium-wide notices to the
covered operators that are not regular, and never publishes ATAP-only or
non-bus notices. Expired cards leave the observation without a cancellation
claim. These pages carry no publication date, so none is stored.

`trenitalia-scioperi` watches Trenitalia's Infomobilità notice page. The page's
region tags decide local applicability; an untagged notice only qualifies when
its own text describes a national strike by Trenitalia or the FS Group. Notices
restricted to other regions, freight-only notices, and personnel categories with
no passenger-service signal are excluded, so an ambiguous national proclamation
is not published as a local disruption.

## Identity

Bus notices are identified by their canonical permalink. Trenitalia notices are
identified by the page URL plus the AEM component name
(`infomobility_summary_<n>`), which appears in both the HTML and the JSON
rendering of the same page and survives in-place edits. If upstream renames a
permalink or recreates a notice component, an edit appears as a new notice plus
a disappearance — which emits nothing — rather than as a revision; the
extraction version exists to rebaseline if a better identity is found.

## Verified state and limits

Live probes on the deployment host on 2026-09-10 extracted the TPL FVG notice
in force with its detail page and published it as a first-run item, and parsed
the Trenitalia page without error. No Trenitalia strike notice was published
that day, so railway strike markup is verified through fixture-driven tests
against the Internet Archive capture `20260905080112` of the same official
page, which contains a national strike, a Trenitalia Trentino strike and an SAD
Trentino strike. The live probe reports missing rail coverage instead of
claiming it. Component-id stability across upstream edits remains unverified.

## Alternatives

- Classifying from the TPL FVG collection alone: its cards carry a date and a
  label but no applicability, so ATAP-only notices would be published.
- Letting a Flow fetch its own detail pages: violates ADR 0002 and 0005, and
  loses the shared fetcher, gate and atomic commit.
- Reusing `EnrichedChangeFlow`: it runs after comparison, so it cannot decide
  identity, applicability or whether a notice changed.
- Watching Trenitalia's `notizie-infomobilita.model.json`: structured, but an
  internal AEM representation of an official page; the HTML page is the
  published artifact.
- Adding the MIT strike register: excluded by the specification; it is not an
  operator notice and would claim national coverage rfs cannot verify.
