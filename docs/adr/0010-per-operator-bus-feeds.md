# Per-operator bus feeds with disruption naming

Amends ADR 0009's open question. The rail feed keeps its behaviour and only
changes name.

TPL FVG's own alert hub is prose plus links, not a collection: the bus notices
live on the operators' own sites. One bus feed would therefore need a
multi-origin fetch plan, with one operator's markup break taking the whole bus
feed down with it. The project's own model is the opposite: a Source is one
upstream resource and one feed, fetched on its own.

## Decision

- Each operator gets its own Source and feed, on its own origin, with its own
  failure. The consortium collection stays a separate feed, because a
  consortium-wide notice affects every operator and the operators do not
  republish it.
- `internal/sources/notices` owns the shared behaviour — permalink identity, an
  observed edit as an `[Aggiornato · ...]` item with the same identity, and no
  item when a notice leaves the list. Each operator package only decodes its own
  page into notices and fails when the page no longer has the structure it
  expects.
- Identity is the operator's own notice permalink. No parser derives identity
  from a title, a date or a body digest.
- Naming follows the scope: feeds, paths and Go packages use disruption
  vocabulary rather than the Italian word for strikes. That word stays only
  where it is upstream data (a page path, a summary URL) or notice text.

## What each collection allows

| Feed | Collection | Rule |
| --- | --- | --- |
| `arriva-udine` | the operator's notice endpoint | The endpoint also serves the whole archive, so the feed reads a fixed newest window. A response without notices fails the poll instead of emptying the baseline. |
| `trieste-trasporti` | the notice page | Only the cards above the archive marker are in force. The marker is required: without it the parser cannot tell an expired card from a current one, and it fails rather than republish old notices. |
| `apt-gorizia` | the summary of the changes in force | Only the notices and route diversions the summary links are published. A page that is neither the summary nor a list of notices fails the poll. |

## Consequences

- The old strike-only consortium feed and the machinery that existed to read it —
  strike validity badges, strike hours, guaranteed-service windows and the
  engine's secondary-page fetch contract — are gone rather than reimplemented:
  each covered operator publishes its own notices, and nothing else used that
  contract.
- The renamed feed paths stop existing. A subscriber has to subscribe again;
  nothing rewrites the stored history, which keeps its source IDs' rows but is
  no longer served. That is acceptable for feeds a reader adds by URL, and it is
  recorded in the README.
- Four bus feeds mean four independent polls. The operator sites are polled
  hourly like everything else, and each one is a single request except the
  consortium feed, which fetches one page per in-force notice.
- The Arriva Udine feed carries notices, not bodies: the endpoint leaves the
  notice text on the page, so the description carries the title, the date and
  the permalink. A future detail fetch would extend that Source alone.
