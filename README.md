# rfs

rfs watches a set of hardcoded web resources and serves each one as its own RSS 2.0 feed, so you can subscribe to publishers that provide no feed of their own.

## Run

```sh
go run ./cmd/rfs
```

By default the server listens on `:14298` and Sources poll every hour unless they declare a source-specific interval. Feeds are served at:

- `/` — HTML index listing every source
- `/feeds/meltzer-5-star-matches.xml` — RSS 2.0 feed
- `/feeds/meltzer-5-star-matches.html` — HTML view of the feed
- `/feeds/ptg.xml` — latest `/ptg/` threads via the board catalog
- `/feeds/ptg.html` — HTML view of the `/ptg/` feed
- `/feeds/film.xml` — latest `/film/` threads via the board catalog
- `/feeds/film.html` — HTML view of the `/film/` feed
- `/feeds/seadex.xml` — Discord-style SeaDex recommendation diffs
- `/feeds/seadex.html` — HTML view of the SeaDex diffs
- `/feeds/one-piece.xml` — new One Piece chapters from TCB Scans
- `/feeds/one-piece.html` — HTML view of the One Piece chapters

### SeaDex

The first successful complete poll silently establishes a baseline. Subsequent
polls publish changed sections with `-` removed and `+` added lines: Best, Alt,
Unmuxed Best, Notes, Tags, Dual Audio, Incomplete, Comparisons, and release
identities/links. Replacements within the same release group are also detected.
Each observed revision gets a new GUID, including reversions and removals.

Titles and optional cover URLs come from SeaDex's public `anilist` collection,
requested only for changed anime. No direct AniList API access is required
(the previous implementation's AniList POST returned HTTP 403 on this host).
Missing titles fall back to AniList IDs; unsafe cover URLs are skipped.
Failed pages or metadata queries preserve the baseline and feed for retry.
HTTP access blocks are reported, not bypassed.

An empty feed after the first poll is expected: it is waiting for future changes,
not reconstructing past Discord messages. To verify live fetching and metadata
without touching your database:

```sh
RFS_TEST_SEADEX_LIVE=1 go test ./internal/sources/seadex -run TestLiveSeaDex -v -count=1
```

SeaDex uses the default poll interval (one hour; adjustable with `-interval`).
This is an **observed-change feed**, not a Discord message mirror: edits made
and reverted between polls cannot be recovered. Dates are observation times.
The baseline and all emitted updates persist in SQLite across restarts; history
is currently retained without automatic pruning. Extraction-version upgrades
silently rebaseline rather than publishing code-induced changes.

### One Piece chapters

One Piece announces each chapter once. The first successful poll publishes only
the newest chapter; after that, new chapters appear in order, including releases
that appeared while TCB Scans was unreachable. Items are links only: rfs never
fetches chapter pages or images, and it does not repair links that a publisher
later breaks.

The archive page lists every chapter back to chapter 1 and is fetched in full
each poll. Failures, throttling, and unrecognized pages leave the checkpoint and
the served feed untouched for retry on the next poll. Deduplication lives in the
database: deleting it re-initializes the feed with the newest chapter. See
`docs/adr/0010-announcement-checkpoints.md`.

To verify live access and parsing without touching your database:

```sh
RFS_TEST_ONEPIECE_LIVE=1 go test ./internal/sources/onepiece -run TestLive -v -count=1
```

### A Closer Listen recommendations

`/feeds/acloserlisten.xml` (or `.html`) publishes all currently embedded Bandcamp
albums on the first successful poll, then only previously unseen album IDs.
Returning albums and editorial changes do not create duplicates. Titles come
from Bandcamp player metadata; links lead to the recommendations page.

The source reads the public WordPress API rather than challenge-protected HTML.
Metadata requests run sequentially and only for new albums. Any failed metadata
request leaves the entire batch and checkpoint uncommitted for retry. No extra
cache, credentials, database migration, or background worker is needed. The
checkpoint retains album IDs across restarts; deleting the database resets it.

## Storage

State is stored in a SQLite database under the OS user cache directory by default:

- Linux: `$XDG_CACHE_HOME/rfs/rfs.sqlite`, or `~/.cache/rfs/rfs.sqlite`
- macOS: `~/Library/Caches/rfs/rfs.sqlite`
- Windows: `%LocalAppData%\\rfs\\rfs.sqlite`

Use `-db :memory:` for a throwaway in-memory database, or `-db /path/to/rfs.sqlite` to choose a specific file. The `-interval` flag controls the default poll interval for Sources without an override. Run `go run ./cmd/rfs -h` for all flags (`-addr`, `-interval`, `-domain-spacing`, `-self-update`, `-self-update-interval`, `-self-update-timeout`, `-version`). Self-update checks run independently of source polling every 10 minutes by default, with a 30-second deadline per check.