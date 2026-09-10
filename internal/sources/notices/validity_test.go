package notices

import (
	"testing"
	"time"
)

func dayDate(year int, month time.Month, day int) Date {
	return Date{Time: time.Date(year, month, day, 0, 0, 0, 0, rome), Precision: PrecisionDay}
}

func mintueDate(t time.Time) Date {
	return Date{Time: t, Precision: PrecisionMinute}
}

func romeTime(year int, month time.Month, day, hour, minute int) time.Time {
	return time.Date(year, month, day, hour, minute, 0, 0, rome)
}

func TestCutoffStepsBackTwoCalendarMonths(t *testing.T) {
	for _, tc := range []struct {
		name string
		at   time.Time
		want string
	}{
		{"mid month", romeTime(2026, time.September, 10, 12, 0), "2026-07-10"},
		{"month end clamps to a short month", romeTime(2026, time.April, 30, 9, 0), "2026-02-28"},
		{"leap year keeps 29 February", romeTime(2028, time.April, 29, 9, 0), "2028-02-29"},
		{"month end of a long month", romeTime(2026, time.March, 31, 9, 0), "2026-01-31"},
		{"year rolls back", romeTime(2026, time.January, 15, 9, 0), "2025-11-15"},
		{"late evening in Rome is still the local day", romeTime(2026, time.September, 9, 23, 30), "2026-07-09"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Cutoff(tc.at).In(rome).Format("2006-01-02")
			if got != tc.want {
				t.Fatalf("Cutoff(%s) = %s, want %s", tc.at.In(rome).Format(time.RFC3339), got, tc.want)
			}
		})
	}
}

func TestCutoffUsesTheRomeCalendarDayOfTheObservation(t *testing.T) {
	// 23:30 UTC is already the next day in Rome, and the cutoff has to follow
	// the operators' calendar rather than the poller's UTC clock.
	at := time.Date(2026, time.September, 9, 23, 30, 0, 0, time.UTC)
	got := Cutoff(at).In(rome).Format("2006-01-02")
	if got != "2026-07-10" {
		t.Fatalf("Cutoff = %s, want the Rome-local 2026-07-10", got)
	}
}

func TestEligibilitySurvivesTheDaylightSavingSwitch(t *testing.T) {
	// The clock moves forward on 29 March 2026 and back on 25 October 2026. A day
	// bound still covers the whole local day it names, and the cutoff still lands
	// on the right local date.
	notice := Notice{ID: "https://example.com/a", Link: "https://example.com/a", Title: "Linea 9 deviata"}
	notice.Validity = Validity{Start: dayDate(2026, time.March, 29), End: dayDate(2026, time.March, 29)}
	if !Eligible(notice, romeTime(2026, time.March, 29, 23, 30)) {
		t.Fatal("the notice was treated as over before its own day ended")
	}
	if Eligible(notice, romeTime(2026, time.March, 30, 9, 0)) {
		t.Fatal("the notice outlived the day it named")
	}
	if got := Cutoff(romeTime(2026, time.October, 25, 23, 30)).In(rome).Format("2006-01-02"); got != "2026-08-25" {
		t.Fatalf("Cutoff = %s, want the local 2026-08-25", got)
	}
}

func TestEligibleKeepsNoticesTheCollectionDoesNotExpire(t *testing.T) {
	at := romeTime(2026, time.September, 10, 12, 0)
	titles := Notice{ID: "https://example.com/a", Link: "https://example.com/a", Title: "Linea 9 deviata"}
	for _, tc := range []struct {
		name   string
		notice Notice
		want   bool
	}{
		{"no dates at all", titles, true},
		{"publication inside the window", withPublished(titles, dayDate(2026, time.July, 10)), true},
		{"publication exactly at the cutoff", withPublished(titles, dayDate(2026, time.July, 10)), true},
		{"publication one day before the cutoff", withPublished(titles, dayDate(2026, time.July, 9)), false},
		{"unknown publication is not age", titles, true},
		{"open-ended start is not an end", withValidity(titles, Validity{Text: "dal 7 febbraio 2022", Start: dayDate(2022, time.February, 7)}), true},
		{"start already past but open", withValidity(titles, Validity{Start: dayDate(2026, time.August, 1)}), true},
		{"single day still running", withValidity(titles, Validity{Start: dayDate(2026, time.September, 10), End: dayDate(2026, time.September, 10)}), true},
		{"single day over", withValidity(titles, Validity{Start: dayDate(2026, time.September, 9), End: dayDate(2026, time.September, 9)}), false},
		{"range still running", withValidity(titles, Validity{Start: dayDate(2026, time.September, 1), End: dayDate(2026, time.September, 30)}), true},
		{"range over", withValidity(titles, Validity{Start: dayDate(2026, time.August, 1), End: dayDate(2026, time.August, 20)}), false},
		{"end without a day is not an end", withValidity(titles, Validity{Text: "fino alla fine degli interventi", End: Date{Text: "la fine degli interventi"}}), true},
		{"event still ahead", withValidity(titles, Validity{Start: dayDate(2026, time.September, 20), End: dayDate(2026, time.September, 20)}), true},
		{"range opening later", withValidity(titles, Validity{Start: dayDate(2026, time.October, 1), End: dayDate(2026, time.October, 5)}), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Eligible(tc.notice, at); got != tc.want {
				t.Fatalf("Eligible = %v, want %v for %#v", got, tc.want, tc.notice.Validity)
			}
		})
	}
}

func withPublished(n Notice, published Date) Notice {
	n.Published = published
	return n
}

func withValidity(n Notice, validity Validity) Notice {
	n.Validity = validity
	return n
}

func TestValidityFromTextReadsTheStatedWindow(t *testing.T) {
	for _, tc := range []struct {
		name     string
		text     string
		start    string
		end      string
		wantText string
	}{
		{
			name:     "single day event",
			text:     "Avviso di sciopero di 4 ore per il giorno 10 settembre 2026",
			start:    "2026-09-10",
			end:      "2026-09-10",
			wantText: "per il giorno 10 settembre 2026",
		},
		{
			name:     "open ended start",
			text:     "Zona Carnia, variazioni sui servizi dal 13 aprile 2026",
			start:    "2026-04-13",
			wantText: "dal 13 aprile 2026",
		},
		{
			name:     "start with a time and a weekday",
			text:     "Provvedimenti in vigore dalle 8:00 di luned\u00ec 24 agosto 2026 fino alla fine degli interventi previsti.",
			start:    "2026-08-24",
			wantText: "dalle 8:00 di luned\u00ec 24 agosto 2026",
		},
		{
			name:     "explicit range sharing one month",
			text:     "Deviazioni dal 13 al 20 aprile 2026 in via Carducci",
			start:    "2026-04-13",
			end:      "2026-04-20",
			wantText: "dal 13 al 20 aprile 2026",
		},
		{
			name:     "explicit range with both dates",
			text:     "Cantiere dal 01/08/2026 al 30/09/2026",
			start:    "2026-08-01",
			end:      "2026-09-30",
			wantText: "dal 01/08/2026 al 30/09/2026",
		},
		{
			name:     "end only",
			text:     "Termini aperti fino a sabato 31 ottobre 2026.",
			end:      "2026-10-31",
			wantText: "fino a sabato 31 ottobre 2026",
		},
		{
			name:     "bare date states nothing",
			text:     "Moraro, fermate sospese per processione il 08/09/2026",
			wantText: "",
		},
		{
			name:     "update date states nothing",
			text:     "Gorizia, centro intermodale, aggiornamento del 16-06-2026",
			wantText: "",
		},
		{
			name:     "headline without dates",
			text:     "Chiusura di via Sant'Anastasio causa lavori: deviazione per le linee 28, 64 e 30",
			wantText: "",
		},
		{
			name:     "start without a year stays unresolved",
			text:     "Novit\u00e0 dal 6 luglio sui servizi extraurbani",
			wantText: "dal 6 luglio",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ValidityFromText(tc.text)
			if tc.wantText == "" {
				if got.Text != "" || got.Start.Time.IsZero() == false || got.End.Time.IsZero() == false {
					t.Fatalf("ValidityFromText(%q) = %#v, want nothing", tc.text, got)
				}
				return
			}
			if got.Text != tc.wantText {
				t.Fatalf("text = %q, want %q", got.Text, tc.wantText)
			}
			assertDay(t, "start", got.Start, tc.start)
			assertDay(t, "end", got.End, tc.end)
		})
	}
}

func assertDay(t *testing.T, label string, got Date, want string) {
	t.Helper()
	if want == "" {
		if !got.Time.IsZero() {
			t.Fatalf("%s = %v, want no bound", label, got.Time)
		}
		return
	}
	if got.Time.IsZero() {
		t.Fatalf("%s is unresolved, want %s", label, want)
	}
	if formatted := got.Time.In(rome).Format("2006-01-02"); formatted != want {
		t.Fatalf("%s = %s, want %s", label, formatted, want)
	}
}
