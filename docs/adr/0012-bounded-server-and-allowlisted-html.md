# Server connections are bounded, and Flow HTML is allowlisted

rfs serves its feeds from a long-lived process whose listener is reachable on
every interface, and its feed pages render markup that Flows build. Both
assumed well-behaved peers. Bounds on what rfs fetches are in 0002.

## Decision

- **Server connections are bounded.** `newHTTPServer` sets ReadHeaderTimeout,
  ReadTimeout, WriteTimeout, and IdleTimeout. Without them a few slow clients
  that never finish their headers can pin every goroutine and file descriptor
  the process has.
- **Flow-built HTML is allowlisted.** `ItemDescriptionsHTML` descriptions pass
  through `safeHTML` before `template.HTML`. Flows still escape upstream text
  themselves (0008); the allowlist is the second line of defence, so one Flow
  mistake cannot turn the feed into a script or request sink. Unknown elements
  are unwrapped to text, `script`/`style`/`iframe`/`svg`-class elements are
  dropped with their subtree, and only HTTP(S) image sources, image alt/width,
  and a small style vocabulary survive.

## Considered options

- **A reverse proxy for timeouts** (rejected): the deployed host has no
  supervisor or proxy; the binary must protect itself.
- **Allowlist in each Flow instead of at render time** (rejected): the flag was
  already a per-Source promise with nothing enforcing it. Render-time
  enforcement covers every current and future Flow.
- **Sanitize by escaping everything** (rejected): the change feeds rely on
  rendered diffs, so the markup has to survive.

## Consequences

- A slow or silent client cannot hold a connection open indefinitely.
- Rich descriptions lose attributes outside the allowlist. Flows only emit what
  survives today, and the sanitizer tests pin that markup.
- Quarantined and rejected items are visible in the log; there is no metrics
  endpoint yet.
