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
- `/feeds/tpl-fvg-scioperi.xml` — TPL FVG bus strike notices for Arriva Udine, Trieste Trasporti and APT Gorizia
- `/feeds/tpl-fvg-scioperi.html` — HTML view of the TPL FVG strike feed
- `/feeds/trenitalia-scioperi.xml` — Trenitalia strike notices affecting travel in Friuli Venezia Giulia
- `/feeds/trenitalia-scioperi.html` — HTML view of the Trenitalia strike feed

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

### Strike feeds

Two operator-confirmed strike feeds exist, and nothing else in the strike space:

- `tpl-fvg-scioperi` covers Arriva Udine, Trieste Trasporti and APT Gorizia
  (including Monfalcone), plus consortium-wide TPL FVG notices. ATAP-only
  notices, unrelated transport modes, the expired notices on the same page and
  the standing guarantee windows are never published as items.
- `trenitalia-scioperi` covers Trenitalia passenger services that affect travel
  in Friuli Venezia Giulia: notices from the region's tag, routes serving Udine,
  Trieste, Gorizia or Monfalcone, and national notices whose scope includes the
  region. Notices limited to other regions, freight-only activity and personnel
  categories that the notice does not connect to covered passenger services are
  excluded.

A notice qualifies when the operator says covered services **may** be disrupted
by a specified strike. That is a statement about potential disruption, not a
report that a specific departure is cancelled: rfs never claims that a train or
bus will not run, and notices that disappear or expire emit nothing. Only an
explicit operator revocation produces a revocation item.

Both feeds publish their scope in every title, for example
`[Bus · Arriva Udine] Udine, sciopero di 4 ore — 10 settembre 26 (ore 17:00-21:00, 17:45-21:45)`
or `[Treni · Nazionale] Dalle ore 21:18 ... sciopero nazionale del personale del
Gruppo FS, Trenitalia, Trenitalia Tper e Trenord`. An observed edit emits an
`[Aggiornato · ...]` item and an explicit revocation an `[Revocato · ...]` item,
each with a new revision GUID; unchanged and formatting-only polls emit nothing.
Descriptions keep the operator's original Italian wording.

Unlike the SeaDex feed, the first successful poll **publishes** the notices that
are already in force instead of starting from a silent baseline, so a new
subscriber is not left with an empty feed.

The TPL FVG collection page carries dates and validity badges but not
applicability, so rfs fetches each in-force notice's page before comparing
anything. Every required page is fetched through the shared fetcher and the
domain gate; if any of them fails, the whole poll fails and the stored baseline
and feed are preserved. Polling uses the default hourly interval
(`-interval`) for both feeds, and RSS dates are observation times — the strike
window itself is in the title and description.

To verify live extraction without touching your database:

```sh
RFS_TEST_TPL_FVG_LIVE=1 go test ./internal/sources/tplfvg -run TestLiveTPLFVG -v -count=1
RFS_TEST_TRENITALIA_LIVE=1 go test ./internal/sources/trenitaliascioperi -run TestLiveTrenitalia -v -count=1
```

The rail probe reports when Trenitalia publishes no strike notice at all: the
feed then has nothing to show rather than inventing coverage.

## Storage

State is stored in a SQLite database under the OS user cache directory by default:

- Linux: `$XDG_CACHE_HOME/rfs/rfs.sqlite`, or `~/.cache/rfs/rfs.sqlite`
- macOS: `~/Library/Caches/rfs/rfs.sqlite`
- Windows: `%LocalAppData%\\rfs\\rfs.sqlite`

Use `-db :memory:` for a throwaway in-memory database, or `-db /path/to/rfs.sqlite` to choose a specific file. The `-interval` flag controls the default poll interval for Sources without an override. Run `go run ./cmd/rfs -h` for all flags (`-addr`, `-interval`, `-domain-spacing`, `-self-update`, `-self-update-interval`, `-self-update-timeout`, `-version`). Self-update checks run independently of source polling every 10 minutes by default, with a 30-second deadline per check.