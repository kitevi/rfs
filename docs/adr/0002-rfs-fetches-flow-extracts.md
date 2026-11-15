# rfs fetches with one client; Flows decode and extract

rfs fetches every page a feed is built from and hands the bytes to the
Source's Flow. A Flow performs no IO. It turns bytes into items and, when it
needs more, names the URLs rfs should fetch next: further collection pages,
identity pages, metadata queries and item pages. Every request goes through
one client, so politeness, bounds and access live in one place, and every Flow
is a pure function tested against saved response bodies.

This record also holds what 0005 (Flows decode page bytes) and the fetching
half of 0012 used to.

## Decision

- **Flows receive bytes.** The fetcher returns the response body as `Page`.
  Each Flow decodes the representation its Source publishes: HTML Flows use
  `rfs.ParseHTML` and `internal/dom`, JSON Flows use `encoding/json`. A
  Flow's fixture is the raw response body.
- **Requests are conditional where a 304 is enough.** rfs sends the stored ETag
  and Last-Modified. A 200 replaces both, because it describes the body it
  returned; keeping a validator it omitted would pin later requests to content
  rfs never parsed. A 304 keeps them. Announcement and paginated Sources fetch
  their collection in full every poll (0010). An extraction-version change
  (0003), or an item page that could not be dated (0014), makes the next
  request unconditional.
- **Every request is bounded.** A request has 30 seconds, and a body over
  16 MiB fails the fetch. rfs holds whole pages in memory, so an endless
  response fails the poll, which keeps the stored feed, instead of growing
  until the process dies. A poll cycle may run until the next one is due
  (0013); these bounds stop one stalled connection from consuming it.
- **One domain is polled politely.** Polls of one domain run one at a time, and
  follow-up requests book the domain's next slot (`Poller.SecondaryFetch`),
  so a whole poll stays within `-domain-spacing` (10 seconds) instead of
  bursting. HTTP 429 and 503 back the domain off by Retry-After, or by a delay
  doubling from a minute to an hour without one. Any other failure keeps the
  normal spacing, so a layout change does not delay the retry that fixes it.
- **rfs names itself over Chrome's TLS handshake.** Every request carries rfs's
  User-Agent. The connection opens with Chrome's ClientHello (uTLS), because
  CDNs challenge Go's own handshake whatever the request says, and the
  response is read over HTTP/2 or HTTP/1.1, as the server chose. rfs does not
  solve challenges, run page scripts or send a browser's User-Agent. A site
  that still refuses rfs fails the poll, the log names the error, and the feed
  keeps what it had.
- **Self-update keeps Go's TLS.** It downloads and runs a new binary, so it has
  its own standard-library client (0015), whose TLS gets Go's security fixes
  first.

## Measured on 15 November 2026

boomkat.com answered Go's handshake with a Cloudflare challenge (HTTP 403,
`cf-mitigated: challenge`) whatever the request carried: rfs's User-Agent, a
bot-style one or Chrome's; over HTTP/2 or HTTP/1.1; with exactly the headers
curl sends. curl got the page only with Chrome's User-Agent. Chrome's
handshake got it with rfs's own. The sites of the other eleven Sources, and
the Bandcamp and YouTube follow-ups, served the same page to either handshake.
A Closer Listen's own site also challenges ordinary clients, which is why that
Source reads the WordPress API.

## Considered options

- **Each Flow fetches itself** (rejected): every Flow would repeat HTTP and
  politeness, and Flow tests would need the network.
- **Separate HTML and JSON Flow interfaces** (rejected): the poller would pick
  extractors at runtime, and each new representation would add an interface.
- **Report blocks and wait** (the earlier rule): Boomkat's feed would stay
  empty, and more sites challenge Go's handshake over time.
- **A browser's User-Agent** (rejected): it hides rfs, and the challenge
  follows the handshake, not the header.
- **curl for challenged Sources** (rejected): a runtime dependency on the
  server's curl, whose handshake passes only with a browser's User-Agent.
- **Chrome's handshake only for Sources that opt in** (rejected): a third
  client and a code change for each site that starts challenging, for no
  difference on the sites that accept either handshake.

## Consequences

- uTLS adds five modules (uTLS, brotli, compress, x/crypto, x/text): `go.mod`
  requires three directly and 16 in total.
- Chrome's hello offers compressed certificates, so the brotli and zstd
  decoders under uTLS read bytes from every server rfs connects to, before its
  certificate is verified. uTLS v1.8.2 asks for versions with reachable flaws
  (GO-2026-6888, GO-2026-6865); `go.mod` requires brotli v1.3.0 and compress
  v1.20.4 instead. Run govulncheck after every uTLS update.
- Only the handshake is Chrome's; the HTTP/2 frames are Go's. A CDN that checks
  them too will challenge rfs again, and matching them would mean forking Go's
  HTTP/2 client.
- uTLS's copy of Chrome's handshake ages with Chrome. When a site that worked
  starts challenging rfs, update uTLS first.
- Each request opens its own connection, so spaced follow-ups pay a handshake
  each, and rfs keeps no connection pool.
- Source requests ignore `HTTP_PROXY` and `HTTPS_PROXY`; self-update still
  honours them.
- Opt-in live probes fetch through this client (`rfstest.FetchLive`,
  `rfs.NewHTTPFetcher(nil)`), so a probe passes only where rfs would.
- Flows are tested against saved bodies and fake fetchers; the client is tested
  on its own, against local TLS servers for both protocols.
