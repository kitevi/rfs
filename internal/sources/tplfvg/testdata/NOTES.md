# tplfvg fixtures

Captured on 2026-09-10 from the deployment host with the rfs user agent
(`rfs/0.1 (+https://github.com/ppowo/rfs)`) while implementing the discovery
gate in `docs/tech-spec-fvg-strike-feeds.md`. Upstream URLs:

- `https://tplfvg.it/it/servizi/scioperi/` (collection)
- `https://tplfvg.it/it/servizi/scioperi/<slug>/` (notice detail)

## Real captures

Each file below is the upstream response with one mechanical edit: the
`<div id="main-content">` element (and its subtree) was extracted and wrapped in
a minimal HTML document. Nothing inside the element was changed, and no notice
text, status image, link or date was edited.

| Fixture | Upstream page |
| --- | --- |
| `scioperi_index.html` | the strike collection; one active notice (`10set26`) and six expired ones |
| `scioperi_index_empty.html` | same page with the notice-card container removed to represent "no current notices" |
| `sciopero_udine_10set26.html` | Arriva Udine 4-hour strike, active on 2026-09-10 |
| `sciopero_nazionale_18mag26.html` | national 24-hour strike affecting Trieste Trasporti and APT Gorizia while Arriva Udine runs regular service |
| `sciopero_tplfvg_3ott25.html` | consortium-wide notice ("Possibili cancellazioni e ritardi ... di Tpl Fvg") |

## Synthesized variants

These are the real captures above with the exact replacements listed here, so
the tests can cover states that were not present upstream on the capture date.
They are derived fixtures, not upstream evidence.

| Fixture | Base | Replacements |
| --- | --- | --- |
| `sciopero_udine_10set26_updated.html` | `sciopero_udine_10set26.html` | `17:00` to `18:00`, `17:45` to `18:45`, and an `Aggiornamento: la fascia oraria dello sciopero è stata modificata.` paragraph inserted as the first body paragraph |
| `sciopero_udine_10set26_reformatted.html` | `sciopero_udine_10set26.html` | whitespace and paragraph-break formatting only; every visible character is unchanged |
| `sciopero_udine_10set26_revoked.html` | `sciopero_udine_10set26.html` | `Lo sciopero è stato revocato dall'associazione sindacale proclamante.` inserted as the first body paragraph |
| `sciopero_udine_10set26_atap_only.html` | `sciopero_udine_10set26.html` | title, summary and body scope changed to ATAP Pordenone, with Arriva Udine, APT Gorizia and Trieste Trasporti described as regular |
| `scioperi_index_national.html` | card markup copied from `scioperi_index.html` | one active card for `18mag26` ("Sciopero nazionale di 24 ore") |
| `scioperi_index_consortium.html` | card markup copied from `scioperi_index.html` | one active card for `3ott25` ("Sciopero nazionale di 24 ore") |
| `scioperi_index_atap_only.html` | card markup copied from `scioperi_index.html` | one active card for `10set26` labelled "Pordenone, sciopero di 4 ore" |

## Not represented

No upstream notice on the capture date was explicitly revoked, revoked on the
collection card, ATAP-only, or malformed. Those states exist only through the
derived fixtures and the live probe in `live_test.go`.
