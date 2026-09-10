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
- `/feeds/arriva-udine.xml` — Arriva Udine service notices
- `/feeds/arriva-udine.html` — HTML view of the Arriva Udine feed
- `/feeds/trieste-trasporti.xml` — Trieste Trasporti service notices
- `/feeds/trieste-trasporti.html` — HTML view of the Trieste Trasporti feed
- `/feeds/apt-gorizia.xml` — APT Gorizia service notices
- `/feeds/apt-gorizia.html` — HTML view of the APT Gorizia feed
- `/feeds/trenitalia-disruptions.xml` — Trenitalia disruptions affecting travel in Friuli Venezia Giulia
- `/feeds/trenitalia-disruptions.html` — HTML view of the Trenitalia disruption feed

The transport feeds were renamed from their `scioperi` paths when the scope
widened to disruption, so those URLs no longer exist and a reader has to
subscribe again.

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
silently rebaseline rather than publishing code-induced changes; the rail feed is
the documented exception (ADR 0009).

### Transport feeds

Four independent operator feeds cover disruptions affecting travel in Friuli
Venezia Giulia:

- `arriva-udine`: the newest notices from Arriva Udine's notice endpoint.
- `trieste-trasporti`: Trieste Trasporti's active notice cards, excluding its archive.
- `apt-gorizia`: APT's linked active notices for Gorizia and Monfalcone.
- `trenitalia-disruptions`: Trenitalia passenger disruptions affecting FVG,
  including covered routes and applicable national notices.

Disruptions include strikes, works, timetable changes, diversions, suspensions,
stop closures and operational incidents. Disappearance is not cancellation.
Each feed publishes additions and edits with revision-qualified GUIDs; unchanged
notices emit nothing. Feed-history behavior is unchanged: these are announcement
feeds, not live departure boards.

Bus items carry structured publication and validity metadata separately from
human-readable descriptions. HTML labels publication dates explicitly; unknown
dates stay unknown. RSS uses a non-future minute-precision publication timestamp
when available for a new notice, otherwise the observation timestamp. Updates
retain their observation timestamp. Day-only dates do not invent midnight.

Bus notices with a stated end in the past or a publication date older than two
calendar months are neither announced nor served. Unknown dates alone do not
exclude a notice. Rail keeps its announcement history and its operator dates in
descriptions. Details: [transport feed decisions](docs/adr/0009-broadened-disruption-scope.md).

There is no compatibility reader or automatic repair for pre-metadata bus items.
Before deploying over such a database, back it up and arrange a one-time repair
or bus-only state reset separately. Merely clearing cached HTTP validators does
not replace already-published items. No production data is reset by this code.

Probe all four operators without using the production database:

```sh
RFS_TEST_TRANSPORT_LIVE=1 go test ./internal/sources -run TestLiveTransportFeeds -v -count=1
```

## Storage

State is stored in a SQLite database under the OS user cache directory by default:

- Linux: `$XDG_CACHE_HOME/rfs/rfs.sqlite`, or `~/.cache/rfs/rfs.sqlite`
- macOS: `~/Library/Caches/rfs/rfs.sqlite`
- Windows: `%LocalAppData%\\rfs\\rfs.sqlite`

Use `-db :memory:` for a throwaway in-memory database, or `-db /path/to/rfs.sqlite` to choose a specific file. The `-interval` flag controls the default poll interval for Sources without an override. Run `go run ./cmd/rfs -h` for all flags (`-addr`, `-interval`, `-domain-spacing`, `-self-update`, `-self-update-interval`, `-self-update-timeout`, `-version`). Self-update checks run independently of source polling every 10 minutes by default, with a 30-second deadline per check.