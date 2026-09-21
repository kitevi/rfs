# Durable announcement checkpoints for release feeds

Amends ADR 0008 for Sources whose Flow implements `AnnouncementFlow`. Projection
and change-comparison Sources keep their current behavior.

TCB Scans publishes One Piece chapters and frequently goes down. The One Piece
Flow must announce each chapter exactly once, publish only the newest chapter
when it first observes the archive, and recover releases that appeared while the
site was unreachable. `ChangeFlow` compares one observation with the previous
one, so an archive that regresses or omits a chapter forgets it, and a
revision-based GUID makes retries depend on a successful commit.

## Decision

- `AnnouncementFlow.Extract` emits stable chapter identities.
  `Evaluate(checkpoint, current)` compares a complete observation with a
  Flow-owned checkpoint and returns the next checkpoint plus the items to
  publish. Both remain pure and perform no IO.
- The first successful observation announces only the newest chapter and stores
  it as the baseline. Later observations announce every chapter above the
  baseline that has not been announced yet, in ascending order, so an outage
  spanning several releases publishes all of them once.
- The checkpoint keeps the full announced set, not just the highest chapter: a
  chapter published late (out of order) is still announced, and a chapter that
  disappears from the archive is not forgotten or re-announced. Chapters at or
  below the baseline are ignored, so a backfilled archive cannot flood the feed.
- SQLite stores the opaque checkpoint in `announcement_state`. Announced items,
  the checkpoint, and the fetch validators commit in one transaction, so a
  failed poll cannot consume a release without publishing it.
- Announcement Sources always request the full collection page. A conditional
  304 would skip the comparison that both initializes and advances the
  checkpoint, and the bytes are cheap compared with a missed release.
- GUIDs stay stable (`one-piece:chapter:<n>`) and announced rows are inserted
  with `INSERT OR IGNORE`, so a replayed decision cannot duplicate an entry and
  published text is immutable.

## Consequences

What is new is decided by the checkpoint, not by what the current page still
shows, so archive gaps heal on the next successful poll. Losing the database
resets the baseline: the next observation re-initializes and announces only the
newest chapter. Announcement history is retained without pruning, matching the
change-feed precedent. The Flow rejects unsupported checkpoint versions instead
of guessing, and no chapter content, image, or fallback provider is fetched.
Preference for the canonical link is the first occurrence in document order,
which resolves duplicate or review-style URLs deterministically.
