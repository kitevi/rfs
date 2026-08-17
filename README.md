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
- `/feeds/ptg.xml` — latest Desuarchive `/ptg/` threads
- `/feeds/ptg.html` — HTML view of the `/ptg/` feed
- `/feeds/film.xml` — latest 4plebs `/film/` threads
- `/feeds/film.html` — HTML view of the `/film/` feed
- `/feeds/tildes-comp.xml` — most-upvoted `~comp` topics on Tildes over the past year
- `/feeds/tildes-comp.html` — HTML view of the Tildes feed
- `/feeds/osmer-rain-trieste.xml` — OSMER coastal-zone (Z4) forecast sections that mention rain around Trieste
- `/feeds/osmer-rain-trieste.html` — HTML view of the OSMER rain feed
- `/feeds/ptv-remote-italy-jobs.xml` — open PTV Logistics positions remote-eligible for Italy
- `/feeds/ptv-remote-italy-jobs.html` — HTML view of the PTV feed

State is stored in a SQLite database under the OS user cache directory by default:

- Linux: `$XDG_CACHE_HOME/rfs/rfs.sqlite`, or `~/.cache/rfs/rfs.sqlite`
- macOS: `~/Library/Caches/rfs/rfs.sqlite`
- Windows: `%LocalAppData%\\rfs\\rfs.sqlite`

Use `-db :memory:` for a throwaway in-memory database, or `-db /path/to/rfs.sqlite` to choose a specific file. The `-interval` flag controls the default poll interval for Sources without an override. Run `go run ./cmd/rfs -h` for all flags (`-addr`, `-interval`, `-domain-spacing`, `-self-update`, `-self-update-interval`, `-self-update-timeout`, `-version`). Self-update checks run independently of source polling every 10 minutes by default, with a 30-second deadline per check.