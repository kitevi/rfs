# rfs reads GitHub releases itself; unused paths and copies are removed

Amends 0004, 0007, 0008 and 0009.

rfs imported go-selfupdate to find a release and download two files. Everything
that makes an update safe was already rfs's own code: the no-downgrade rule,
the checksum check, the extraction limits and the `.bak` swap. For that one
call the binary carried 17 extra modules, among them API clients for GitLab and
Gitea and an OpenPGP package that govulncheck reported as reachable
(GO-2026-5932), and about 6 MB.

The same review found code with no production caller, and helpers each Flow had
copied.

## Decision

- **Self-update talks to GitHub with the standard library.** `githubReleases`
  lists the newest releases from the public API and picks the highest published
  version that carries both `rfs_<version>_<os>_<arch>.tar.gz` and
  `checksums.txt`. Drafts, pre-releases and tags that are not three numeric
  fields are ignored. A release that is still uploading lacks an asset, so the
  previous complete release stays the candidate until a later check. Assets are
  fetched from their `browser_download_url`; every hop, redirects included,
  must be HTTPS. `GITHUB_TOKEN`, when set, authenticates the listing only, as
  the library did. Versions are compared as three numbers (0004); a version
  with a suffix is never eligible.
- **The update workflow is unchanged.** `Updater` still downloads checksums
  first, verifies the archive, extracts `rfs` within its size limits, and swaps
  through `FileSwapper`. The asset names in `.goreleaser.yaml` remain the
  contract, so a build that still uses the library installs a release built
  after this change.
- **Change enrichment is one GET per batch.** `EnrichedChangeFlow` names a URL
  (`EnrichmentURL`) instead of a request with an optional body, and the fetcher
  loses `FetchJSON`. The direct AniList POST that 0008 kept as an alternative
  had no Source: SeaDex uses its hosted `anilist` collection.
- **History is written only by `CommitHistory`.** `MergeHistory` was called
  only by tests; it is now a test seed over `CommitHistory` with no rebuild.
  Pruning reads the live set it has just written instead of receiving it as
  parameters.
- **Flows share their HTML helpers.** Attribute, class, text and element
  lookups live in `internal/dom`. `rfs.ParseHTML` (0002) stays where it is.
  Helpers that encode one page's rules, such as Meltzer's cell text, stay with
  their Flow.
- **Feeds are routed by `net/http` patterns.** `GET /{$}` and
  `GET /feeds/{file}` replace hand-written path splitting. A request with
  another method to an unknown path now gets 404 instead of 405, and a path
  that is not clean is redirected.
- `ItemPresenter` and `Item.DateLabel` had no implementation and are gone.

## Considered options

- **Upgrade go-selfupdate** (rejected): 1.7.1 adds two more modules and keeps
  the surface rfs does not use.
- **Follow `/releases/latest/download/checksums.txt` without the API**
  (rejected for now): no rate limit and no JSON, but it trusts GitHub's
  "latest" pointer instead of the highest version. Revisit if the API's
  unauthenticated limit of 60 requests an hour per address ever bites; rfs
  makes six.
- **Merge `Item` and `ExtractedItem`** (rejected): it changes the stored
  baseline encoding for little gain.

## Consequences

- `go.mod` went from 28 modules to 11, two of them direct:
  `golang.org/x/net` and `modernc.org/sqlite`.
- rfs depends on the shape of GitHub's releases JSON. An opt-in probe runs the
  production workflow against the real feed with a fake swapper:
  `RFS_TEST_SELFUPDATE_LIVE=1 go test ./internal/rfs -run TestLiveSelfUpdate -v -count=1`.
- There is still no automatic rollback (0004).
- No Flow's extraction output, GUIDs or checkpoint encoding changed, so no
  extraction version was bumped and stored feeds are not rebuilt.
