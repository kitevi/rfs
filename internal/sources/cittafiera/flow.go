// Package cittafiera extracts films from the Cine Città Fiera homepage.
//
// The homepage lists every film the cinema has loaded, including titles whose
// first screening is months away. Each card links to a film page whose day
// buttons are the real run. A film is announced once its earliest day is today
// or no more than two weeks later, in Europe/Rome. Farther films are remembered
// and published when that day arrives. The 35MM badge is on every card and is
// not a format, so it is left off the title.
package cittafiera

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	_ "time/tzdata"

	"golang.org/x/net/html"

	"github.com/kitevi/rfs/internal/rfs"
)

const (
	// PageURL is the homepage rfs polls. It lists the whole loaded program.
	PageURL = "https://www.cine-cittafiera.com/"

	// HumanURL is the daily schedule a reader opens from the feed.
	HumanURL = "https://www.cine-cittafiera.com/programmazione/"

	// siteHost is the only host whose film links are accepted.
	siteHost = "www.cine-cittafiera.com"

	// ExtractVersion is the derivation version for this Flow.
	ExtractVersion = 1

	// CheckpointVersion is the announcement checkpoint schema version.
	CheckpointVersion = 1

	// guidPrefix makes a film's Webtic id recoverable from its GUID.
	guidPrefix = "citta-fiera:film:"

	// holdDays is how far ahead a first screening may be and still be announced.
	holdDays = 14
)

// rome is the cinema's civil timezone. Screening days are calendar dates there.
var rome = mustRome()

var (
	idPattern = regexp.MustCompile(`^[0-9]+$`)
	months    = [...]string{"", "Gen", "Feb", "Mar", "Apr", "Mag", "Giu", "Lug", "Ago", "Set", "Ott", "Nov", "Dic"}
)

// Flow extracts homepage films and decides which are due.
type Flow struct{}

var (
	_ rfs.AnnouncementFlow         = Flow{}
	_ rfs.DatedAnnouncementFlow    = Flow{}
	_ rfs.AnnouncementIdentityFlow = Flow{}
)

// Version reports the derivation version.
func (Flow) Version() int { return ExtractVersion }

// Extract returns every film card on the homepage, in page order. Repeated
// links to one film yield a single item. The properties badge is not part of
// the title.
func (Flow) Extract(page rfs.Page) ([]rfs.ExtractedItem, error) {
	doc, err := rfs.ParseHTML(page)
	if err != nil {
		return nil, fmt.Errorf("cittafiera: parse page: %w", err)
	}

	var items []rfs.ExtractedItem
	seen := map[string]bool{}
	for _, anchor := range anchors(doc) {
		id, ok := filmID(attr(anchor, "href"))
		if !ok || seen[id] {
			continue
		}
		title := collapse(textWithoutClass(anchor, "properties"))
		if title == "" || isMoreLink(title) {
			continue
		}
		seen[id] = true
		items = append(items, rfs.ExtractedItem{
			GUID:  guidPrefix + id,
			Title: title,
			Link:  filmURL(id),
		})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("cittafiera: no films")
	}
	return items, nil
}

// checkpoint is the announcement state the Flow owns. Held films have been
// seen but their first screening is still more than two weeks away.
type checkpoint struct {
	Version   int               `json:"version"`
	Announced []string          `json:"announced"`
	Held      map[string]string `json:"held,omitempty"`
}

// filmNote is carried from ResolveIdentity to EvaluateAt, then removed.
type filmNote struct {
	First    string `json:"first"`
	Director string `json:"director,omitempty"`
	Duration string `json:"duration,omitempty"`
}

// PlanIdentities reports films that still need a film page: anything not yet
// announced, including films already held, so a moved screening day is re-read.
func (Flow) PlanIdentities(raw json.RawMessage, current []rfs.ExtractedItem) ([]rfs.ExtractedItem, []rfs.ExtractedItem, error) {
	announced := map[string]bool{}
	if len(raw) > 0 {
		state, err := decodeCheckpoint(raw)
		if err != nil {
			return nil, nil, err
		}
		for _, id := range state.Announced {
			if !idPattern.MatchString(id) {
				return nil, nil, fmt.Errorf("cittafiera: invalid checkpoint id %q", id)
			}
			announced[id] = true
		}
	}

	ready := make([]rfs.ExtractedItem, 0, len(current))
	var pending []rfs.ExtractedItem
	seen := map[string]bool{}
	for _, item := range current {
		id, err := itemID(item.GUID)
		if err != nil {
			return nil, nil, err
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		ready = append(ready, item)
		if !announced[id] {
			pending = append(pending, item)
		}
	}
	if len(ready) == 0 {
		return nil, nil, fmt.Errorf("cittafiera: no films observed")
	}
	return ready, pending, nil
}

// IdentityURL is the film page for one pending item, built from its id.
func (Flow) IdentityURL(item rfs.ExtractedItem) (string, error) {
	id, err := itemID(item.GUID)
	if err != nil {
		return "", err
	}
	return filmURL(id), nil
}

// ResolveIdentity reads the earliest screening day, director, and duration.
func (Flow) ResolveIdentity(page rfs.Page, item rfs.ExtractedItem) (rfs.ExtractedItem, error) {
	if _, err := itemID(item.GUID); err != nil {
		return rfs.ExtractedItem{}, err
	}
	note, err := readFilmPage(page)
	if err != nil {
		return rfs.ExtractedItem{}, err
	}
	encoded, err := json.Marshal(note)
	if err != nil {
		return rfs.ExtractedItem{}, fmt.Errorf("cittafiera: encode film note: %w", err)
	}
	item.Metadata = string(encoded)
	return item, nil
}

// Evaluate decides at the current time. The poller uses EvaluateAt with its clock.
func (f Flow) Evaluate(raw json.RawMessage, current []rfs.ExtractedItem) (rfs.AnnouncementDecision, error) {
	return f.EvaluateAt(time.Now(), raw, current)
}

// EvaluateAt announces films whose first screening is today or no more than
// two weeks later in Europe/Rome. Farther films stay in the checkpoint until
// a later poll. A held film that leaves the homepage is forgotten.
func (Flow) EvaluateAt(now time.Time, raw json.RawMessage, current []rfs.ExtractedItem) (rfs.AnnouncementDecision, error) {
	films, err := observedFilms(current)
	if err != nil {
		return rfs.AnnouncementDecision{}, err
	}
	announced := map[string]bool{}
	held := map[string]string{}
	if len(raw) > 0 {
		state, err := decodeCheckpoint(raw)
		if err != nil {
			return rfs.AnnouncementDecision{}, err
		}
		for _, id := range state.Announced {
			if !idPattern.MatchString(id) {
				return rfs.AnnouncementDecision{}, fmt.Errorf("cittafiera: invalid checkpoint id %q", id)
			}
			announced[id] = true
		}
		for id, day := range state.Held {
			if !idPattern.MatchString(id) {
				return rfs.AnnouncementDecision{}, fmt.Errorf("cittafiera: invalid held id %q", id)
			}
			if _, err := time.Parse("2006-01-02", day); err != nil {
				return rfs.AnnouncementDecision{}, fmt.Errorf("cittafiera: invalid held day %q", day)
			}
			held[id] = day
		}
	}

	onPage := map[string]bool{}
	var announcements []rfs.ExtractedItem
	for _, film := range films {
		onPage[film.id] = true
		if announced[film.id] {
			continue
		}
		note, err := takeNote(film.item)
		if err != nil {
			return rfs.AnnouncementDecision{}, err
		}
		if !withinWindow(now, note.when) {
			held[film.id] = note.when.Format("2006-01-02")
			continue
		}
		delete(held, film.id)
		announced[film.id] = true
		item := film.item
		item.Description = filmDescription(note)
		item.Metadata = ""
		item.Link = filmURL(film.id)
		announcements = append(announcements, item)
	}
	for id := range held {
		if !onPage[id] {
			delete(held, id)
		}
	}
	encoded, err := encodeCheckpoint(checkpoint{
		Version:   CheckpointVersion,
		Announced: sortedIDs(announced),
		Held:      heldOrNil(held),
	})
	if err != nil {
		return rfs.AnnouncementDecision{}, err
	}
	return rfs.AnnouncementDecision{Checkpoint: encoded, Announcements: announcements}, nil
}

type observed struct {
	id   string
	item rfs.ExtractedItem
}

func observedFilms(items []rfs.ExtractedItem) ([]observed, error) {
	if len(items) == 0 {
		return nil, fmt.Errorf("cittafiera: no films observed")
	}
	seen := map[string]bool{}
	films := make([]observed, 0, len(items))
	for _, item := range items {
		id, err := itemID(item.GUID)
		if err != nil {
			return nil, err
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		films = append(films, observed{id: id, item: item})
	}
	return films, nil
}

func readFilmPage(page rfs.Page) (filmNote, error) {
	doc, err := rfs.ParseHTML(page)
	if err != nil {
		return filmNote{}, fmt.Errorf("cittafiera: parse film page: %w", err)
	}
	days, err := screeningDays(doc)
	if err != nil {
		return filmNote{}, err
	}
	return filmNote{
		First:    days[0].Format("2006-01-02"),
		Director: labeled(doc, "Regista:"),
		Duration: labeled(doc, "Durata:"),
	}, nil
}

func screeningDays(doc *html.Node) ([]time.Time, error) {
	seen := map[string]bool{}
	var days []time.Time
	for _, anchor := range anchors(doc) {
		parsed, err := url.Parse(strings.TrimSpace(attr(anchor, "href")))
		if err != nil {
			continue
		}
		raw := parsed.Query().Get("d")
		fields := strings.Fields(raw)
		if len(fields) == 0 {
			continue
		}
		day := fields[0]
		if seen[day] {
			continue
		}
		when, err := time.Parse("2006-01-02", day)
		if err != nil {
			return nil, fmt.Errorf("cittafiera: unrecognized day %q", raw)
		}
		seen[day] = true
		days = append(days, when)
	}
	if len(days) == 0 {
		return nil, fmt.Errorf("cittafiera: film page has no day")
	}
	slices.SortFunc(days, time.Time.Compare)
	return days, nil
}

func labeled(doc *html.Node, label string) string {
	for _, strong := range elements(doc, "strong") {
		if collapse(textContent(strong)) != label {
			continue
		}
		if strong.Parent == nil {
			return ""
		}
		text := strings.TrimPrefix(collapse(textContent(strong.Parent)), label)
		return strings.TrimSpace(text)
	}
	return ""
}

func withinWindow(now, first time.Time) bool {
	today := dateOnly(now.In(rome))
	opening := dateOnly(first)
	return !opening.After(today.AddDate(0, 0, holdDays))
}

func dateOnly(t time.Time) time.Time {
	year, month, day := t.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func filmDescription(note resolvedNote) string {
	parts := []string{formatDay(note.when)}
	if note.Director != "" {
		parts = append(parts, note.Director)
	}
	if note.Duration != "" {
		parts = append(parts, note.Duration)
	}
	return strings.Join(parts, ", ")
}

func formatDay(day time.Time) string {
	return fmt.Sprintf("%d %s %d", day.Day(), months[day.Month()], day.Year())
}

func takeNote(item rfs.ExtractedItem) (resolvedNote, error) {
	if item.Metadata == "" {
		return resolvedNote{}, fmt.Errorf("cittafiera: film %s was not classified", item.GUID)
	}
	var note filmNote
	if err := json.Unmarshal([]byte(item.Metadata), &note); err != nil {
		return resolvedNote{}, fmt.Errorf("cittafiera: decode film note: %w", err)
	}
	when, err := time.Parse("2006-01-02", note.First)
	if err != nil {
		return resolvedNote{}, fmt.Errorf("cittafiera: film %s has no day", item.GUID)
	}
	return resolvedNote{filmNote: note, when: when}, nil
}

// resolvedNote is a film note whose first day has been parsed.
type resolvedNote struct {
	filmNote
	when time.Time
}

func itemID(guid string) (string, error) {
	id, ok := strings.CutPrefix(guid, guidPrefix)
	if !ok || !idPattern.MatchString(id) {
		return "", fmt.Errorf("cittafiera: unexpected item GUID %q", guid)
	}
	return id, nil
}

func filmURL(id string) string {
	return "https://" + siteHost + "/scheda-film/?id_pro=" + id
}

// filmID parses a film-card link into its Webtic id.
func filmID(href string) (string, bool) {
	trimmed := strings.TrimSpace(href)
	if trimmed == "" {
		return "", false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", false
	}
	if parsed.Host != "" && parsed.Host != siteHost {
		return "", false
	}
	if parsed.Scheme != "" && parsed.Scheme != "https" && parsed.Scheme != "http" {
		return "", false
	}
	path := strings.TrimSuffix(parsed.Path, "/")
	if path != "/scheda-film" && path != "scheda-film" {
		return "", false
	}
	id := parsed.Query().Get("id_pro")
	if !idPattern.MatchString(id) {
		return "", false
	}
	return id, true
}

func isMoreLink(title string) bool {
	switch strings.ToLower(title) {
	case "scopri di più", "scopri di piu":
		return true
	default:
		return false
	}
}

func anchors(root *html.Node) []*html.Node {
	return elements(root, "a")
}

func elements(root *html.Node, tag string) []*html.Node {
	var found []*html.Node
	var walk func(*html.Node)
	walk = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == tag {
			found = append(found, node)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return found
}

func textWithoutClass(node *html.Node, class string) string {
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.ElementNode && hasClass(current, class) {
			return
		}
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return builder.String()
}

func hasClass(node *html.Node, class string) bool {
	for _, token := range strings.Fields(attr(node, "class")) {
		if token == class {
			return true
		}
	}
	return false
}

func attr(node *html.Node, name string) string {
	for _, attribute := range node.Attr {
		if attribute.Key == name {
			return attribute.Val
		}
	}
	return ""
}

func textContent(node *html.Node) string {
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			builder.WriteString(current.Data)
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return builder.String()
}

func collapse(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func sortedIDs(set map[string]bool) []string {
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func heldOrNil(held map[string]string) map[string]string {
	if len(held) == 0 {
		return nil
	}
	return held
}

func encodeCheckpoint(state checkpoint) (json.RawMessage, error) {
	if state.Announced == nil {
		state.Announced = []string{}
	}
	encoded, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("cittafiera: encode checkpoint: %w", err)
	}
	return encoded, nil
}

func decodeCheckpoint(raw json.RawMessage) (checkpoint, error) {
	var state checkpoint
	if err := json.Unmarshal(raw, &state); err != nil {
		return checkpoint{}, fmt.Errorf("cittafiera: decode checkpoint: %w", err)
	}
	if state.Version != CheckpointVersion {
		return checkpoint{}, fmt.Errorf("cittafiera: unsupported checkpoint version %d", state.Version)
	}
	return state, nil
}

func mustRome() *time.Location {
	loc, err := time.LoadLocation("Europe/Rome")
	if err != nil {
		panic(err)
	}
	return loc
}
