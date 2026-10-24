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
- `/feeds/mal-stack-82158.xml` — observed changes to a MyAnimeList Interest Stack
- `/feeds/mal-stack-82158.html` — HTML view of the stack changes
- `/feeds/mal-stack-38814.xml` — observed changes to The Next Sakuga Shows stack
- `/feeds/mal-stack-38814.html` — HTML view of the stack changes
- `/feeds/acloserlisten.xml` — new recommended releases from A Closer Listen
- `/feeds/acloserlisten.html` — HTML view of the recommendations
- `/feeds/coached-essays.xml` — essays from Rohan Mahtani
- `/feeds/coached-essays.html` — HTML view of the essays
- `/feeds/visionario.xml` — new films at Visionario and Cinema Centrale
- `/feeds/visionario.html` — HTML view of the films
- `/feeds/citta-fiera.xml` — new films at Cine Città Fiera
- `/feeds/citta-fiera.html` — HTML view of the films

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
each poll. A page that cannot be read, and throttling, leave the checkpoint and
the served feed untouched for retry on the next poll. A single chapter whose
link and heading disagree is skipped instead of rejecting the page, so one odd
anchor cannot freeze the whole feed; that chapter is retried on the next poll.
Deduplication lives in the database: deleting it re-initializes the feed with
the newest chapter. See `docs/adr/0010-announcement-checkpoints.md` and
`docs/adr/0011-quarantine-unreadable-items.md`.

To verify live access and parsing without touching your database:

```sh
RFS_TEST_ONEPIECE_LIVE=1 go test ./internal/sources/onepiece -run TestLive -v -count=1
```

### MyAnimeList Interest Stacks

- `mal-stack-82158` watches *New Anime IPs with Exceptional Production Value*.
- `mal-stack-38814` watches *The Next Sakuga Shows*.

Each source has its own baseline and feed. The first successful complete poll
silently establishes a baseline. Later polls publish one item per affected
anime: **Added**, **Removed**, or **Notes updated**, with the changed span of the
curator's notes as `-`/`+` lines. Membership changes are always published, even
when notes are empty. Anime with unknown release dates or episode counts are
still tracked.

Only membership and note text are compared. Reordering, titles, covers, scores,
airing status, restack counts and the stack's own introduction are ignored. Note
text is canonicalized so entity and whitespace-only differences produce no
noise; link targets stay visible as inert text.

This is an **observed-change feed**: edits made and reverted between polls cannot
be recovered, and item dates are discovery times, not upstream edit times. An
unrecognized, incomplete, or paginated page fails closed, so a temporary layout
change or access block preserves the baseline and feed for retry instead of
publishing false removals. Access blocks are reported, not bypassed. History is
retained without automatic pruning, and extraction-version upgrades silently
rebaseline rather than publishing code-induced changes.

To verify live access and parsing without touching your database:

```sh
RFS_TEST_MALSTACK_LIVE=1 go test ./internal/sources/malstack -run TestLiveMALStack -v -count=1
```

### A Closer Listen recommendations

`/feeds/acloserlisten.xml` (or `.html`) publishes embedded Bandcamp releases.
The first successful poll announces every release on the page. Later polls
announce only releases that have not been announced before. An album player is
that album. A track player that belongs to an album is that same album. A track
with no album is announced once, as the track. A YouTube embed is announced
once, as that video. A bold artist followed by an italic title is announced
once, as that album. An album named that way and an album player with the same
artist and title (ignoring a trailing format word such as EP, or a bracketed
note) are one release: the player is announced when both are on the page, and
whichever appears later is not announced again. Each album player is read once
to learn its name. Other players are ignored. Returning releases and editorial
changes do not create duplicates.
Titles come from Bandcamp player metadata; links lead to the recommendations
page.

The source reads the public WordPress API rather than challenge-protected HTML.
Metadata requests run sequentially and only for new albums. A release whose
metadata cannot be read is quarantined: the rest of the batch publishes, the
checkpoint does not record the skipped release, and the next poll retries it. A
metadata service that throttles rfs still stops the whole poll, so Retry-After
reaches the domain gate. No extra cache, credentials, database migration, or
background worker is needed. The checkpoint retains album IDs across restarts;
deleting the database resets it.

### Coached essays

`/feeds/coached-essays.xml` (or `.html`) publishes the All essays list from
coached.com. The site serves no feed of its own. The essay index lists that
catalog in a `<noscript>` fallback under the heading All essays; the same list
is what the All essays tab shows once JavaScript runs. Newsletter issue pages
are not items: each one is one current essay, one older essay, and other links.

Essays on the index have titles and links but no dates. rfs fetches each
essay's own page once, reads its `article:published_time`, and remembers it, so
later polls fetch only essays it has not dated yet. The site sometimes moves an
essay to a new slug; the moved essay gets a new GUID, but it keeps its original
date instead of sorting above this week's essay. An essay whose page cannot be
read is dated by when rfs first saw it, and the next poll tries its page again.

The fallback is text to x/net/html, so the Flow parses it a second time and
reads only the All essays section. A page that stops listing that section fails
the poll, leaving the served feed untouched.

### Visionario and Cinema Centrale

`/feeds/visionario.xml` (or `.html`) publishes new films from the archive at
visionario.movie/films/. Cinema Visionario and Cinema Centrale in Udine share
that site. Centrale is closed for renovation, so current showtimes name
Visionario; a showtime row that names Centrale is included in the description
when one appears.

The first successful poll remembers every film already on the archive page and
publishes nothing. Later polls announce each new film page once. A prima
visione or anteprima is an item. An evento is remembered and left unpublished.
The item link is the film page. The description carries the opening date and
any hall named on that page, for example `dal 15 Ott 2026, Visionario`. The
feed date is when rfs first saw the page.

A film page that is not a release or an event, or a release page with no
opening date, is quarantined: the other films in the same poll still publish and
the unreadable film is retried on the next poll. The homepage day switcher is
not used. The archive's first page holds about ten films, so a multi-day outage
can let a film scroll off before the next poll.

To verify live access and parsing without touching your database:

```sh
RFS_TEST_VISIONARIO_LIVE=1 go test ./internal/sources/visionario -run TestLive -v -count=1
```

### Cine Città Fiera

`/feeds/citta-fiera.xml` (or `.html`) publishes new films from the homepage at
cine-cittafiera.com. The homepage lists every film the cinema has loaded,
including titles whose first screening is months away. The film page's day
buttons are the real run.

A film is announced once, when its earliest day is today or no more than two
weeks later, in Europe/Rome. The first poll publishes every film already
inside that window. A film farther out is remembered and published when the
window reaches it. Avengers: Doomsday, whose page lists 16–20 December, stays
out until 2 December. The item link is the film page. The description is the
first day plus the director and duration, for example `16 Dic 2026, Dave Green,
01:44`. The 35MM badge is not part of the title.

A film page with no screening day is quarantined: every other film in the same
poll is still classified and announced, and the unreadable film is retried on
the next poll. A held film that leaves the homepage is forgotten. The daily
schedule page is not the source: it shows one day at a time.

To verify live access and parsing without touching your database:

```sh
RFS_TEST_CITTAFIERA_LIVE=1 go test ./internal/sources/cittafiera -run TestLive -v -count=1
```

## Storage

State is stored in a SQLite database under the OS user cache directory by default:

- Linux: `$XDG_CACHE_HOME/rfs/rfs.sqlite`, or `~/.cache/rfs/rfs.sqlite`
- macOS: `~/Library/Caches/rfs/rfs.sqlite`
- Windows: `%LocalAppData%\\rfs\\rfs.sqlite`

Use `-db :memory:` for a throwaway in-memory database, or `-db /path/to/rfs.sqlite` to choose a specific file. The `-interval` flag controls the default poll interval for Sources without an override. Run `go run ./cmd/rfs -h` for all flags (`-addr`, `-interval`, `-domain-spacing`, `-self-update`, `-self-update-interval`, `-self-update-timeout`, `-version`). Self-update checks run independently of source polling every 10 minutes by default, with a 30-second deadline per check.