# Rebuild accumulated history from saved extraction inputs

Amends 0003 and 0007. A full re-fetch only re-derives threads still present in
upstream's live catalog. Retained dead threads otherwise keep stale derived
fields indefinitely, or until normal pruning removes them.

## Decision

Persist each matched thread's extraction inputs in `Item.Metadata`, the existing
opaque, flow-owned storage field. For ptg and film this is the JSON encoding of
`catalogThread`, not the whole catalog. The merge path (`mergeHistoryTx`, reached via `MergeHistory` and
`CommitHistory`) persists and upserts metadata as well as rendered fields. No new database column is required, and
metadata is not emitted in history feeds.

A flow implements `HistoryRebuilder.RebuildStored(Item) (Item, error)`. Both fresh
extraction and reconstruction call the same `extractThread` function. Rebuilding
recomputes titles, descriptions, links, and observed reply counts from saved
inputs, not from previously rendered text. The input representation is a stable
storage contract: future changes must continue decoding earlier representations.
Only captured inputs can be reconstructed; adding a previously unrecorded input
still requires an explicit compatibility policy.

On an extraction-version mismatch, a successful full-page poll calls
`CommitHistory`. In one SQLite transaction it:

1. Rebuilds stored rows not superseded by freshly extracted items, preserving
   rows whose saved inputs no longer decode.
2. Merges current items and replaces live membership using only current GUIDs
   (an empty successful observation reuses the stored live set, so a parseable
   catalog gap never clears it).
3. Prunes according to the existing history policy.
4. Saves HTTP validators and the current extraction version.

A merge, prune, checkpoint, or identity-changing rebuild rolls back this
transaction; a retry therefore sees the old version and retries rebuilding.
GUID and publication date must remain unchanged. A row whose saved input no
longer decodes is instead preserved and skipped (logged) so one bad row never
wedges the source: each future bump retries it, and a fresh observation
still overwrites the row as usual. An archived row never
becomes live merely because it was rebuilt. Matching-version polls skip rebuilds;
304, throttling, and extraction-failure behavior is unchanged. Snapshot-replace
and change-feed paths remain unchanged.

## Legacy rows

Existing archived rows have no saved inputs; they cannot be fully reconstructed.
For these rows only, ptg and film repair a leading legacy board marker while
preserving all other fields, then save `{"legacy":true}` in metadata. Later
rebuilds leave marked rows untouched, even if their remaining title contains
another board marker. Fresh observations overwrite the marker with actual
inputs. This compatibility branch must remain while old databases can contain
unmarked rows; it is not a new per-field migration framework.

The release bumps ptg from 5 to 6 and film from 4 to 5, forcing input capture and
legacy repair even where the earlier title-format version is already recorded.

## Alternatives

- Title/item text migrations cannot recover discarded inputs and may corrupt
  already-transformed text on later bumps. The title-only prototype is replaced.
- Deleting history on version changes loses the archived links this feature is
  intended to preserve.
- Read-time derivation adds work and failure modes to serving unnecessarily.
- Per-row extraction versions are unnecessary for this bounded history window:
  rebuilding from saved inputs is repeatable and the source checkpoint is atomic.

## Verification

Tests cover both flows' extraction/rebuild round trips, repeated board markers,
legacy compatibility, malformed inputs, metadata persistence, unchanged-version
and non-rebuilder flows, archived/live membership, identity/date preservation,
and rollback when the final checkpoint fails.
