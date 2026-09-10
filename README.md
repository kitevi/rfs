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

Four operator-confirmed feeds cover travel in Friuli Venezia Giulia:

- `arriva-udine`, `trieste-trasporti` and `apt-gorizia` each cover one bus
  operator's own notices: the ones that operator lists as in force, read from
  its own page. APT Gorizia covers the Gorizia network and the Monfalcone urban
  network it runs. ATAP-only notices and notices the operator moved to its
  archive are never published.
- `trenitalia-disruptions` covers Trenitalia passenger services that affect travel
  in Friuli Venezia Giulia: notices from the region's tag, routes serving Udine,
  Trieste, Gorizia or Monfalcone, and national notices whose scope includes the
  region. Every operational disruption qualifies, not only a strike: weather and
  technical incidents, delays, suspensions, cancellations, route limitations,
  diversions and the region's planned-works page are published too. Notices
  limited to other regions, freight-only activity, personnel categories that the
  notice does not connect to covered passenger services, the national
  high-speed delay list and the standing regional information index are
  excluded.

A notice qualifies when an operator statement establishes both covered-service
applicability and an operational impact, whether actual, anticipated or
conditional. That is a statement about disruption, not a report that a specific
departure is cancelled: rfs never claims that a train or bus will not run, and
notices that disappear or expire emit nothing. Only an explicit operator
statement closes a tracked disruption: an explicit withdrawal produces a
`[Revocato · ...]` item, and a title stating that circulation is regular again
produces a `[Ripristinato · ...]` item for a disruption rfs had already
published.

Every feed publishes its scope in the title, for example
`[Bus · Arriva Udine] Udine, sciopero di 4 ore — 10 settembre 26 (ore 17:00-21:00, 17:45-21:45)`
or `[Treni · Nazionale] Dalle ore 21:18 ... sciopero nazionale del personale del
Gruppo FS, Trenitalia, Trenitalia Tper e Trenord`. An observed edit emits an
`[Aggiornato · ...]` item and an explicit revocation an `[Revocato · ...]` item,
each with a new revision GUID; unchanged and formatting-only polls emit nothing.
Descriptions keep the operator's original Italian wording.

Unlike the SeaDex feed, the first successful poll **publishes** the notices that
are already in force instead of starting from a silent baseline, so a new
subscriber is not left with an empty feed.
A disruption that is already over when it is first observed is history, so it is
never announced: only a disruption rfs tracked while it was active can announce
its own end.

The rail feed widened from strikes to all disruption, which is a new extraction
version. An extraction-version upgrade normally rebaselines in silence, and the
SeaDex feed still does. `trenitalia-disruptions` instead compares the stored
baseline with the re-derived observation, so subscribers of the strike-only feed
receive the incidents the wider scope now covers while unchanged strikes stay
silent. See `docs/adr/0009-broadened-disruption-scope.md`.

The TPL FVG collection page carries dates and validity badges but not
applicability, so rfs fetches each in-force notice's page before comparing
anything. Every required page is fetched through the shared fetcher and the
domain gate; if any of them fails, the whole poll fails and the stored baseline
and feed are preserved. Polling uses the default hourly interval
(`-interval`) for every transport feed, and RSS dates are observation times — the strike
window itself is in the title and description.

To verify live extraction without touching your database:

```sh
RFS_TEST_TRANSPORT_LIVE=1 go test ./internal/sources -run TestLiveTransportFeeds -v -count=1
```

That probe polls every transport feed and logs the notices each one decoded,
including a feed that legitimately carries nothing.

The rail probe reports when Trenitalia publishes no applicable notice at all:
the feed then has nothing to show rather than inventing coverage.

Buses are covered per operator, because TPL FVG's own alert hub is prose plus
links rather than a notice collection and the notices live on the operators'
sites. `trieste-trasporti` reads the notices Trieste Trasporti lists as in force
and never its archive, `arriva-udine` reads the newest window of the operator's
own notice endpoint, and `apt-gorizia` reads the notices and route diversions
the operator's summary of changes in force links, which covers the Gorizia
network and the Monfalcone urban network APT Gorizia runs. Each feed stands or falls on
its own poll: one operator's redesign cannot take another feed down. See
`docs/adr/0010-per-operator-bus-feeds.md`.

## Storage

State is stored in a SQLite database under the OS user cache directory by default:

- Linux: `$XDG_CACHE_HOME/rfs/rfs.sqlite`, or `~/.cache/rfs/rfs.sqlite`
- macOS: `~/Library/Caches/rfs/rfs.sqlite`
- Windows: `%LocalAppData%\\rfs\\rfs.sqlite`

Use `-db :memory:` for a throwaway in-memory database, or `-db /path/to/rfs.sqlite` to choose a specific file. The `-interval` flag controls the default poll interval for Sources without an override. Run `go run ./cmd/rfs -h` for all flags (`-addr`, `-interval`, `-domain-spacing`, `-self-update`, `-self-update-interval`, `-self-update-timeout`, `-version`). Self-update checks run independently of source polling every 10 minutes by default, with a 30-second deadline per check.