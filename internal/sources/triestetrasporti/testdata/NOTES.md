# Trieste Trasporti fixtures

`avvisi_20260910.html` is the operator's notice page
(`https://www.triestetrasporti.it/it/avvisi-infomobilita`) captured on
2026-09-10. Nothing was edited.

What the capture establishes:

- Nine cards stand above `#archivio-avvisi`; the archive below it holds the
notices the operator no longer lists as in force. The parser reads only the
former and fails when the marker is missing.
- Every card carries `<time datetime="...">`. The theme writes a wall-clock
reading with a `Z` suffix: every value in this capture is `12:00:00Z`, which is
a rendered date rather than a real instant. The parser therefore takes the
calendar day as written and treats it as the notice's own date at day
precision.
- Validity wording lives in the card text, not in a field. `Provvedimenti in
vigore dalle 8:00 di lunedì 24 agosto 2026 fino alla fine degli interventi
previsti.` yields a start and no end — that is the notice reported as arriving
from August — while `Termini aperti fino a sabato 31 ottobre 2026.` yields an
end, and `Dal 14 settembre in vigore l'orario invernale` yields a start whose
year is not stated, so it stays undecoded and is shown as published.
- No card in the capture states an end that has already passed, because expired
notices live in the archive. The expiry path is covered by the shared tests in
`internal/sources/notices` and by the Arriva Udine single-day event.
