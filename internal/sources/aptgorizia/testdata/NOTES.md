# APT Gorizia fixtures

`avvisi_20260910.html` is the operator's summary of the changes in force
(`https://www.aptgorizia.it/in-evidenza/modifiche-al-servizio-riepilogo-aggiornato-avvisi-attivi-scioperi-variazioni-orari/`)
captured on 2026-09-10. Nothing was edited.

What the capture establishes:

- The summary links six notices in the two categories the feed covers; the
parser publishes those and nothing else on the page.
- The page states no publication date for any of them. The feed therefore
carries none, and the two-month freshness cutoff never applies here: inventing
one from a start date would suppress notices that are still in force.
- The notices do state starts, in `DD-MM-YYYY` form: `... fermata sospesa dal
29-06-2026`, `... a causa lavori dal 28-08-2026`. Those are starts, and they are
shown as such.
- `Moraro, fermate sospese per processione il 08/09/2026` carries a bare date. A
bare date states no window, so it stays unread and the notice stays eligible.
