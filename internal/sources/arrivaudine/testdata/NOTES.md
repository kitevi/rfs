# Arriva Udine fixtures

`notices_20260910.json` is the operator's own notice endpoint
(`https://www.arrivaudine.it/wp-json/wp/v2/notice?per_page=10&orderby=date&order=desc&_fields=id,date,link,title,content`)
captured on 2026-09-10. It was not edited.

The endpoint serves the whole archive, so this newest window doubles as the
evidence for the freshness cutoff. Captured at 2026-09-10 12:00 UTC, the ten
notices span 2026-03-30 to 2026-09-04:

| Notice | Published | Stated window | Announced |
| --- | --- | --- | --- |
| strike, 4 hours, 10 September | 2026-09-04 | `per il giorno 10 settembre 2026` | yes |
| winter timetables | 2026-08-26 | none stated | yes |
| extraurban changes from 6 July | 2026-07-03 | `dal 6 luglio`, no year | outside the cutoff |
| Easter timetables | 2026-03-30 | none stated | outside the cutoff |

What the capture establishes:

- The endpoint dates every notice, to the minute, in the site's own timezone.
- A single-day event is stated as `per il giorno <date>`. That is the only
expiry-shaped wording in the window, and it is what the same-day boundary is
tested with: the strike is live on 10 September and over on the 11th.
- Most notices state no window at all, so nothing beyond the freshness cutoff
may be inferred from their age.
- A start without a year (`dal 6 luglio`) stays undecoded. No fixture was
invented to give it one.

The endpoint also carries a `new` flag on some responses. Nothing here depends
on it: the captured response has no such field on the notices these tests use.
