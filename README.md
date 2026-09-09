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
- `/feeds/ptg.xml` — latest `/ptg/` threads via the 4chan catalog
- `/feeds/ptg.html` — HTML view of the `/ptg/` feed
- `/feeds/film.xml` — latest `/film/` threads via the 4chan catalog
- `/feeds/film.html` — HTML view of the `/film/` feed
- `/feeds/seadex.xml` — Discord-style SeaDex recommendation diffs
- `/feeds/seadex.html` — HTML view of the SeaDex diffs

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

## Storage

State is stored in a SQLite database under the OS user cache directory by default:

- Linux: `$XDG_CACHE_HOME/rfs/rfs.sqlite`, or `~/.cache/rfs/rfs.sqlite`
- macOS: `~/Library/Caches/rfs/rfs.sqlite`
- Windows: `%LocalAppData%\\rfs\\rfs.sqlite`

Use `-db :memory:` for a throwaway in-memory database, or `-db /path/to/rfs.sqlite` to choose a specific file. The `-interval` flag controls the default poll interval for Sources without an override. Run `go run ./cmd/rfs -h` for all flags (`-addr`, `-interval`, `-domain-spacing`, `-self-update`, `-self-update-interval`, `-self-update-timeout`, `-version`). Self-update checks run independently of source polling every 10 minutes by default, with a 30-second deadline per check.