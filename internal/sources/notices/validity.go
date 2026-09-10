package notices

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

// rome is the operators' local calendar. The tzdata import keeps the zone in the
// binary, so the freshness window does not depend on the host shipping one.
var rome = loadRome()

func loadRome() *time.Location {
	location, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		panic(fmt.Sprintf("notices: load Europe/Rome: %v", err))
	}
	return location
}

// FreshnessWindow is how many calendar months a notice's publication date may
// lag the poll and still be announced. It is a news threshold, not a statement
// that the disruption has ended.
const FreshnessWindow = 2

// Precision reports how much of a day an upstream date named.
type Precision int

const (
	// PrecisionUnknown means the wording carried no date this package could read.
	PrecisionUnknown Precision = iota
	// PrecisionDay means the wording named a calendar day and no time of day.
	PrecisionDay
	// PrecisionMinute means the wording named a clock time as well.
	PrecisionMinute
)

// Date is one upstream date together with the wording it came from.
type Date struct {
	// Text is the upstream date as written, kept so a date this package cannot
	// read is still shown the way the operator published it.
	Text string
	// Time is the decoded instant, in the operators' local calendar. Zero when
	// Text carried no date this package could read.
	Time time.Time
	// Precision reports how much of the day Time is precise to.
	Precision Precision
}

// Known reports whether the wording pinned a date this package could read.
func (d Date) Known() bool { return !d.Time.IsZero() }

// Display renders the date for a feed description: the decoded date, or the
// upstream wording when the wording could not be decoded.
func (d Date) Display() string {
	if d.Known() {
		if d.Precision == PrecisionMinute {
			return d.Time.In(rome).Format("02/01/2006 15:04")
		}
		return d.Time.In(rome).Format("02/01/2006")
	}
	return strings.TrimSpace(d.Text)
}

// Validity is the window a notice says it applies to. Both bounds are optional
// and the zero bound means the page did not state one, which is not the same as
// an open-ended window.
type Validity struct {
	// Text is the phrase the bounds were read from.
	Text string
	// Start is when the notice begins to apply.
	Start Date
	// End is when it stops applying.
	End Date
}

// Empty reports whether the notice states no window at all.
func (v Validity) Empty() bool {
	return strings.TrimSpace(v.Text) == "" && !v.Start.Known() && !v.End.Known()
}

// Display renders the window for a feed description, falling back to the
// upstream phrase when the wording carried dates this package could not read.
func (v Validity) Display() string {
	switch {
	case v.Start.Known() && v.End.Known():
		if v.Start.Time.In(rome).Format("2006-01-02") == v.End.Time.In(rome).Format("2006-01-02") {
			return "il " + v.Start.Display()
		}
		return "dal " + v.Start.Display() + " al " + v.End.Display()
	case v.Start.Known():
		return "dal " + v.Start.Display()
	case v.End.Known():
		return "fino al " + v.End.Display()
	default:
		return strings.TrimSpace(v.Text)
	}
}

// Local is the operators' local calendar. Parsers decode wall-clock upstream
// timestamps in it and descriptions render dates in it.
func Local() *time.Location { return rome }

// DayDate records a date the page stated as a calendar day.
func DayDate(t time.Time) Date { return Date{Time: t, Precision: PrecisionDay} }

// MinuteDate records a date the page stated down to the minute.
func MinuteDate(t time.Time) Date { return Date{Time: t, Precision: PrecisionMinute} }

// Cutoff returns the earliest publication day a notice observed at may carry and
// still be announced: FreshnessWindow calendar months back on the local calendar,
// clamped to the last day of the target month.
func Cutoff(at time.Time) time.Time {
	local := at.In(rome)
	observed := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, rome)
	year, month := observed.Year(), int(observed.Month())-FreshnessWindow
	for month < 1 {
		month += 12
		year--
	}
	day := observed.Day()
	if last := time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, rome).Day(); day > last {
		day = last
	}
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, rome)
}

// Eligible reports whether a notice observed at at may be announced. A notice is
// ineligible when the window it states has already ended, or when its own
// publication date is older than the freshness window. Everything the collection
// page does not state stays eligible: a missing date is not an expired notice.
func Eligible(notice Notice, at time.Time) bool {
	return !ended(notice.Validity.End, at) && !stale(notice.Published, at)
}

// ended reports whether a known end bound is already behind at. A day bound
// covers the whole local day it names.
func ended(end Date, at time.Time) bool {
	if !end.Known() {
		return false
	}
	limit := end.Time
	if end.Precision == PrecisionDay {
		local := limit.In(rome)
		limit = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, rome).AddDate(0, 0, 1)
	}
	return !at.Before(limit)
}

// stale reports whether a known publication date is older than the window.
func stale(published Date, at time.Time) bool {
	if !published.Known() {
		return false
	}
	local := published.Time.In(rome)
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, rome)
	return day.Before(Cutoff(at))
}

const (
	months = `(?:gennaio|febbraio|marzo|aprile|maggio|giugno|luglio|agosto|settembre|ottobre|novembre|dicembre)`
	// fullDate is the shapes the operator pages use: an Italian month name, a
	// numeric date, or an ISO date. A bare number is deliberately not a date.
	fullDate = `(?:\d{1,2}\s+` + months + `(?:\s+\d{4})?|\d{4}-\d{2}-\d{2}|\d{1,2}[-/]\d{1,2}[-/]\d{4})`
	weekday  = `(?:lunedì|martedì|mercoledì|giovedì|venerdì|sabato|domenica)`
	article  = `(?:il\s+|lo\s+|la\s+)?`
	// clock lets a phrase such as "dalle 8:00 di luned\u00ec 24 agosto 2026" keep its
	// date inside the same phrase.
	clock   = `(?:\d{1,2}[:.]\d{2}\s*(?:di\s+)?)?`
	onDay   = `(?:` + weekday + `\s+)?`
	startAt = `(?:a\s+partire\s+dal\s+|in\s+vigore\s+dal\s+|dall['’]\s*|dalle\s+|dal\s+)`
	endAt   = `(?:fino\s+al\s+|fino\s+a\s+|sino\s+al\s+|entro\s+il\s+)`
)

// validityPatterns are tried in order: the first phrase that matches decides the
// window, so a range is never mistaken for an open-ended start.
var validityPatterns = []struct {
	re   *regexp.Regexp
	kind validityKind
}{
	{regexp.MustCompile(`(?i)\b(?:a\s+partire\s+dal|in\s+vigore\s+dal|dal)\s+` + article + `(\d{1,2}|` + fullDate + `)\s+(?:al|fino\s+al|fino\s+a|sino\s+al|a)\s+` + article + `(` + fullDate + `)`), validityRange},
	{regexp.MustCompile(`(?i)\b(?:per\s+il\s+giorno|il\s+giorno)\s+` + article + `(` + fullDate + `)`), validityDay},
	{regexp.MustCompile(`(?i)\b(?:` + startAt + `)` + clock + onDay + article + `(` + fullDate + `)`), validityStart},
	{regexp.MustCompile(`(?i)\b(?:` + endAt + `)` + clock + onDay + article + `(` + fullDate + `)`), validityEnd},
}

type validityKind int

const (
	validityRange validityKind = iota
	validityDay
	validityStart
	validityEnd
)

// ValidityFromText reads the window a notice's own wording states: explicit
// ranges, open-ended starts, stated ends, and single-day events. Anything else,
// including a bare date, states no window: the phrase is preserved for readers
// but no bound is invented from it.
func ValidityFromText(text string) Validity {
	for _, pattern := range validityPatterns {
		match := pattern.re.FindStringSubmatchIndex(text)
		if match == nil {
			continue
		}
		phrase := strings.Join(strings.Fields(text[match[0]:match[1]]), " ")
		validity := Validity{Text: phrase}
		switch pattern.kind {
		case validityRange:
			first := parseDateParts(text[match[2]:match[3]])
			second := parseDateParts(text[match[4]:match[5]])
			validity.Start = rangedDate(first, second)
			validity.End = rangedDate(second, first)
		case validityDay:
			validity.Start = parseDateParts(text[match[2]:match[3]]).date()
			validity.End = validity.Start
		case validityStart:
			validity.Start = parseDateParts(text[match[2]:match[3]]).date()
		case validityEnd:
			validity.End = parseDateParts(text[match[2]:match[3]]).date()
		}
		return validity
	}
	return Validity{}
}

// isoDate and numericDate are the two machine-written shapes the pages use.
var (
	isoDate     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	numericDate = regexp.MustCompile(`^\d{1,2}[-/]\d{1,2}[-/]\d{4}$`)
)

// dateParts is a date as written, before the surrounding phrase supplies what a
// single date leaves out.
type dateParts struct {
	text              string
	day, month, year  int
	hasMonth, hasYear bool
}

func parseDateParts(text string) dateParts {
	trimmed := strings.Join(strings.Fields(text), " ")
	parts := dateParts{text: trimmed}
	switch {
	case isoDate.MatchString(trimmed):
		parsed, err := time.ParseInLocation("2006-01-02", trimmed, rome)
		if err != nil {
			return parts
		}
		parts.day, parts.month, parts.year = parsed.Day(), int(parsed.Month()), parsed.Year()
		parts.hasMonth, parts.hasYear = true, true
	case numericDate.MatchString(trimmed):
		parsed, err := time.ParseInLocation("02-01-2006", strings.ReplaceAll(trimmed, "/", "-"), rome)
		if err != nil {
			return parts
		}
		parts.day, parts.month, parts.year = parsed.Day(), int(parsed.Month()), parsed.Year()
		parts.hasMonth, parts.hasYear = true, true
	default:
		fields := strings.Fields(trimmed)
		if len(fields) == 0 {
			return parts
		}
		day, err := strconv.Atoi(fields[0])
		if err != nil {
			return parts
		}
		parts.day = day
		if len(fields) > 1 {
			if month, ok := monthNumber(strings.TrimSuffix(fields[1], ".")); ok {
				parts.month, parts.hasMonth = month, true
			}
		}
		if len(fields) > 2 {
			if year, err := strconv.Atoi(fields[2]); err == nil && year >= 1000 {
				parts.year, parts.hasYear = year, true
			}
		}
	}
	return parts
}

// rangedDate resolves one bound of a range against its partner: a bare day takes
// the partner's month, and a year stated once covers both bounds of the interval.
func rangedDate(parts, partner dateParts) Date {
	if !parts.hasMonth {
		parts.month, parts.hasMonth = partner.month, partner.hasMonth
	}
	if !parts.hasYear {
		parts.year, parts.hasYear = partner.year, partner.hasYear
	}
	return parts.date()
}

func (p dateParts) date() Date {
	if !p.hasMonth || !p.hasYear {
		return Date{Text: p.text}
	}
	when := time.Date(p.year, time.Month(p.month), p.day, 0, 0, 0, 0, rome)
	if when.Day() != p.day || int(when.Month()) != p.month {
		return Date{Text: p.text}
	}
	return Date{Text: p.text, Time: when, Precision: PrecisionDay}
}

func monthNumber(name string) (int, bool) {
	for i, month := range []string{"gennaio", "febbraio", "marzo", "aprile", "maggio", "giugno", "luglio", "agosto", "settembre", "ottobre", "novembre", "dicembre"} {
		if name == month {
			return i + 1, true
		}
	}
	return 0, false
}
