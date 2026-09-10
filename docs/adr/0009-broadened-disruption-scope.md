# Broadened operator scope with an observable extraction-version upgrade

Amends ADR 0003 for the extraction version transition. Every other Source keeps
its current behavior.

The FVG transport feeds started strike-only. Weather and technical incidents,
suspensions, delays, diversions, stop closures and planned works make a journey
unusable just as a strike does, so the scope widens to operator-confirmed
disruption. Widening a Flow changes what `Extract` returns for a fixed page, so
ADR 0003's rule — a version mismatch rebaselines silently — would drop the newly
covered notices for every subscriber that already had a baseline.

## Decision

- **Eligibility.** A notice qualifies when an official operator statement
  establishes both covered-service applicability and an operational impact,
  whether actual, anticipated or conditional. Strikes, weather and technical
  incidents, suspensions, cancellations, delays, route limitations, diversions,
  replacement buses, stop closures and planned works qualify. Generic news,
  other regions, ATAP-only notices, freight-only incidents and standing pages
  without an impact do not.
- **Applicability over vocabulary.** Examine the
  service applicability of a notice, never a keyword blacklist. A title that
  names no operational change is a standing page rather than a disruption, and
  a standing page lists sections for several places, so only its own title can
  establish coverage. That keeps the national high-speed delay list and the
  regional information index out of an FVG feed even though their bodies
  mention FVG lines.
- **Terminal states.** A title stating that the disruption is over is stored as
  `Ripristinato`, distinct from an explicit withdrawal (`Revocato`). A
  disruption that is already over when it is first observed is history: it is
  never announced. Only a disruption tracked while it was active can announce
  its own end, and that announcement keeps the entity's GUID.
- **Opt-in version transition.** `Source.EmitVersionChanges` makes a version
  mismatch compare the stored baseline with the re-derived observation instead
  of rebaselining in silence. The rail feed opts in, so subscribers of the
  strike-only feed receive the disruptions the widened scope now covers while
  unchanged strikes stay silent. The stored payload schema did not change, so
  an older payload is comparison state rather than a difference.

## Consequences

- The opt-in is per Source and deliberately not global: a bump that changes
  every payload would announce a whole feed again, so only a Flow that compares
  an older payload as state may opt in. SeaDex and every projection feed keep
  ADR 0008's silent rebaseline.
- A stable identity plus a stable schema is what makes the upgrade observable.
  A future change to the payload schema has to keep the previous version
  decodable, or the same bump announces every notice as an update.
- The terminal-state rule removes the previous special case that only skipped a
  revoked notice on a first observation.

## Bus coverage

Discovery on the deployment host found that TPL FVG's own alert hub
(`/it/infomobilita/avvisi-sul-servizio/`) is prose plus links, not a notice
collection: the notices live on the three operators' own sites —
`triestetrasporti.it/it/avvisi-infomobilita`, `arrivaudine.it/avvisi/` and APT
Gorizia's "Modifiche al servizio" page. Each is a different origin from the
consortium collection, so the buses became one feed per operator (ADR 0010)
instead of one feed over several origins.
