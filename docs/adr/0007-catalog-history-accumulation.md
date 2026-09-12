# Catalog history accumulation for the board catalog sources

Amends 0001 (current-state projections) for the two board catalog sources only.

`ptg` (`g/catalog.json`) and `film` (`tv/catalog.json`) discover via the
catalog but accumulate observed threads in SQLite instead of replacing the
snapshot. Catalogs list live threads only, so replace semantics can only ever
serve the live thread; dead thread links remain readable via archive-redirect
browser extensions, so keeping them restores the old external-archive
behaviour without archive traffic (which was blocking us).

## Rule

- Store up to 11 rows per source (10 visible + 1 live buffer), prune older
  non-live rows by `(pub_date DESC, guid ASC)`. Live GUIDs are never pruned.
- Serve at most 10 rows: dead threads always; live threads only once
  `replies >= 100` (maturity filter — a fresh general has nothing worth
  reading). Missing `replies` counts as 0.
- Re-observation upserts title/link/description/pub_date/replies/metadata; ordering
  stays on source `time`, not discovery time.
- Rotation gaps (`Extract` error, zero matches) and HTTP 304 preserve history
  and `live_state`; the next successful poll heals.
- `meltzer` keeps 0001 replace semantics.

## Mechanics

- `snapshots.replies INTEGER NOT NULL DEFAULT 0` + `live_state(source_id, guid)`
  (both migrated idempotently).
- `CommitHistory` (rebuild + merge + live replace + prune + fetch/version
  checkpoint) runs in one tx per successful history poll; `MergeHistory`
  remains for seeding and non-rebuild merges. `LoadVisibleHistory` anti-joins
  live with the maturity predicate at serve time. Live is stored-then-hidden:
  dropping it before save would lose catalog threads forever. An empty
  successful observation preserves the stored live set rather than clearing it.
- `ExtractVersion` bumped for the parser change (`ptg` 3→4, `film` 2→3) and
  again when titles began omitting the thread name (`ptg` 4→5, `film` 3→4).
- A version bump alone cannot refresh accumulated rows absent from the catalog.
  `HistoryRebuilder` re-derives them from saved flow-owned inputs. `CommitHistory`
  includes this rebuild, the current merge, pruning, and the fetch/version
  checkpoint in one transaction. See ADR 0009.
