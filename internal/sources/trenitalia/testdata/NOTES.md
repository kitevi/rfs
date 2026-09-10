# Trenitalia fixtures

All fixtures come from Trenitalia's official Infomobilità page
`https://www.trenitalia.com/it/informazioni/Infomobilita/notizie-infomobilita.html`,
trimmed to its `<div class="infomobility-list">` element and wrapped in a minimal
HTML document. Nothing inside the element was edited in the real captures.

## Real captures

| Fixture | Source | Contents |
| --- | --- | --- |
| `notizie_20260905.html` | Wayback Machine raw capture `20260905080112` (`web.archive.org/web/20260905080112id_/...`) | the complete notice list for 2026-09-05: a national strike tagged with all regions including Friuli Venezia Giulia, a Trenitalia Trentino-Alto Adige strike, an SAD (other operator) Trentino strike, real-time disruption bulletins, and the empty per-region info stubs |
| `notizie_20260910.html` | live capture with the rfs user agent | the complete notice list for 2026-09-10: no strike notices are published, only real-time disruption bulletins and empty region stubs |

The live Trenitalia page carries no strike notice on the implementation date, so
the archived capture is the only real evidence of strike-notice markup. It was
fetched from the Internet Archive, not from the deployment host.

## Synthesized variants

Each variant isolates the archived national strike
(`infomobility_summary_548526369`) inside an `infomobility-list` container so the
tests can describe states upstream did not publish on the capture date. They are
derived fixtures, not upstream evidence.

| Fixture | Change |
| --- | --- |
| `notizie_national_updated.html` | strike interval changed from `21:18`/`21:00` to `22:18`/`22:00` in both the title and the body |
| `notizie_national_revoked.html` | `Lo sciopero è stato revocato dall'organizzazione sindacale proclamante.` inserted as the first body paragraph |
| `notizie_national_freight.html` | body replaced with a freight-only statement and the title changed to a freight-sector strike; region tags left unchanged |
| `notizie_national_untagged.html` | `data-region` set to `empty` while the text still describes a national strike |


## Derived variant: restoration

| Fixture | Change |
| --- | --- |
| `notizie_fvg_restored.html` | `notizie_20260910.html` with the captured Venezia - Trieste weather bulletin's title and lead rewritten to the operator's later `circolazione regolare dalle ore 20:00 dopo condizioni meteo critiche` wording, so the restoration lifecycle has a fixture |

The widened scope reads two notices out of `notizie_20260910.html`: the captured
FVG-tagged weather bulletin (`infomobility_summary_1594016248`) and the region's
works page (`infomobility_summary_1720830883`).

## Not represented

No captured notice is specific to Friuli Venezia Giulia alone, no revision is
identified by anything other than the AEM component id in its `id` attributes,
and upstream did not revoke any of the captured notices.
