# Shutdown cancels polls; commits do not observe cancellation

Amends 0004 and 0012.

A poll cycle ran under one fixed two-minute deadline, on a context independent
of the shutdown signal. Every source in a cycle shared it, and the domain gate
spaces follow-up requests 10 seconds apart by default, so one domain fits about
twelve requests per cycle. Cine Città Fiera reads the page of every film it has
not announced on every poll: on 24 October 2026 the homepage listed 23 films,
which needs about four minutes. The deadline cut the poll before it committed,
the error is not quarantinable (cancellation is source-wide, ADR 0011), and the
next cycle repeated the same work. The feed stopped updating and kept serving
its last snapshot with HTTP 200. A Closer Listen's first poll and Visionario
after an outage had the same exposure.

The two-minute deadline existed so that shutdown drain always terminates
(0004). Raising it would have made a self-update wait minutes for a poll's
remaining paced requests.

## Decision

- **The poll context follows shutdown.** `Loop.Run` derives each cycle's
  context from the shutdown signal. A SIGTERM or self-update stops the poll at
  its next request or gate wait; `Run` still waits for the poll to return
  before the process exits or re-execs.
- **Commits ignore cancellation.** Each poll's final write (snapshot, history,
  change state, or announcement checkpoint, together with the fetch validators)
  runs under `context.WithoutCancel`. A poll cancelled before its commit leaves
  nothing behind and is redone after the restart; one cancelled during its
  commit finishes it. Nothing a subscriber saw is lost either way, because every
  commit is already a single transaction (0008, 0010).
- **The cycle deadline defaults to the poll interval.** It only has to stop a
  cycle from running into the next one. Each request still has its own 30-second
  client timeout (0012), so a stalled connection cannot consume it.

## Considered options

- **A per-source deadline scaled by the number of follow-up requests**
  (rejected): the count is only known once the Flow has planned its identities,
  and shutdown would still wait out every remaining request.
- **Defer requests beyond a per-poll budget to the next poll** (rejected): a Flow
  that re-reads the same items first every poll, like Città Fiera's held films,
  would starve the items at the end of the list.
- **Cache held films' dates in the Città Fiera checkpoint** (not now): it would
  cut requests, but the Flow deliberately re-reads held films so a moved
  screening day is noticed. The cost is about 23 polite requests an hour.

## Consequences

- Shutdown and self-update return as soon as the in-flight request or commit
  finishes, instead of after the poll's remaining requests.
- A cycle with many paced requests completes and commits instead of timing out
  forever.
- A poll interrupted by shutdown is repeated after the restart, so it may
  re-fetch pages it already read.
