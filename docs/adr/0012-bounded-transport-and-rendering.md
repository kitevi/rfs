# Transport requests are bounded, and Flow HTML is allowlisted

> **Status: amended by 0013.** The poll cycle deadline now defaults to the poll
> interval; the per-request timeout below is unchanged.

rfs talks to an untrusted network over a long-lived process, and its feed pages
render markup that Flows build. Six assumptions there were wrong by omission.

## Decision

- **Server connections are bounded.** `newHTTPServer` sets ReadHeaderTimeout,
  ReadTimeout, WriteTimeout, and IdleTimeout. The listener is reachable on every
  interface, so without them a few slow clients that never finish their headers
  can pin every goroutine and file descriptor the process has.
- **A response body is bounded.** The fetcher reads at most 16 MiB and fails the
  poll beyond that. rfs holds whole pages in memory; an oversized or endless
  response must fail the fetch, which preserves the stored snapshot, rather than
  grow until the process dies.
- **A request has a deadline of its own.** The default client carries a 30s
  timeout. A poll already has a two-minute deadline, but one stalled connection
  should not consume it, and every follow-up request inside a poll needs the
  same bound.
- **A 200 replaces the stored validators; a 304 preserves them.** The response
  to a modified request describes the body it returned. Keeping a validator it
  omitted would send a stale If-None-Match forever, and a server that honours it
  would answer 304 for content rfs never parsed, freezing the snapshot with no
  error.
- **A poll error is not throttling.** The domain gate applies the normal
  MinSpacing after a failure and leaves the exponential backoff to real
  throttles, so a layout change no longer delays the retry that fixes it.
- **Follow-up requests share the domain gate.** Collection pages, identity
  pages, and metadata queries run through `Poller.SecondaryFetch`
  (`DomainGate.Acquire`), which books the domain's next slot. A whole poll now
  fits inside one domain's spacing budget instead of bursting past it. `Acquire`
  takes no poll lane, so it is safe inside a Run action.
- **Flow-built HTML is allowlisted.** `ItemDescriptionsHTML` descriptions pass
  through `safeHTML` before `template.HTML`. Flows still escape upstream text
  themselves (0008); the allowlist is the second line of defence, so one Flow
  mistake cannot turn the feed into a script or request sink. Unknown elements
  are unwrapped to text, `script`/`style`/`iframe`/`svg`-class elements are
  dropped with their subtree, and only HTTP(S) image sources, image alt/width,
  and a small style vocabulary survive.

## Considered options

- **A reverse proxy for timeouts and limits** (rejected): the deployed host has
  no supervisor or proxy; the binary must protect itself.
- **Allowlist in each Flow instead of at render time** (rejected): the flag was
  already a per-Source promise with nothing enforcing it. Render-time
  enforcement covers every current and future Flow.
- **Sanitize by escaping everything** (rejected): the change feeds rely on
  rendered diffs, so the markup has to survive.

## Consequences

- A hostile or broken upstream can fail a poll, but it cannot hold a connection
  open indefinitely, exhaust memory with one body, or keep a stale validator
  alive across restarts of the same cache row.
- Rich descriptions lose attributes outside the allowlist. Flows only emit what
  survives today, and the sanitizer tests pin that markup.
- Quarantined and rejected items are visible in the log; there is no metrics
  endpoint yet.
