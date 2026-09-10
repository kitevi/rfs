// Package tplfvg watches TPL FVG's operator-confirmed bus strike notices.
//
// The collection page lists every notice with a date, a short label and a
// validity badge, but applicability (Arriva Udine, Trieste Trasporti, APT
// Gorizia versus ATAP Pordenone or unrelated services) only appears on each
// notice's own page. The Flow therefore declares detail pages and lets rfs
// fetch them before extraction.
package tplfvg

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/net/html"

	"github.com/ppowo/rfs/internal/rfs"
)

const (
	// PageURL is the strike collection the Flow watches.
	PageURL = "https://tplfvg.it/it/servizi/scioperi/"
	// HumanURL is the page subscribers should open for context.
	HumanURL = PageURL

	// ExtractVersion is bumped whenever ExtractDetails or Changes output can
	// change for fixed pages, so rfs re-derives instead of trusting a snapshot.
	ExtractVersion = 1

	payloadVersion = 1
	statusActive   = "Attivo"
	statusRevoked  = "Revocato"
)

// noticePathPattern matches the canonical permalink of one strike notice.
// Other cards on the same page use the same markup for unrelated services, so
// the href, not the card class, decides what is a notice.
var noticePathPattern = regexp.MustCompile(`^/it/servizi/scioperi/[^/]+/$`)

var (
	hoursPattern    = regexp.MustCompile(`(?i)dalle ore (\d{1,2}:\d{2}) alle ore (\d{1,2}:\d{2})`)
	dateTextPattern = regexp.MustCompile(`(?i)\b(lunedì|martedì|mercoledì|giovedì|venerdì|sabato|domenica)\s+(\d{1,2}\s+(?:gennaio|febbraio|marzo|aprile|maggio|giugno|luglio|agosto|settembre|ottobre|novembre|dicembre))`)

	// Accept affirmative status statements, not mentions of a possible revocation.
	revocationPattern   = regexp.MustCompile(`(?i)\b(?:revocat[oaie]\s+(?:(?:lo|il|la)\s+|l[’'])?(?:sciopero|agitazione)|(?:sciopero|agitazione)\s+(?:(?:è|sono|risulta|risultano)\s+(?:stat[oaie]\s+)?)?revocat[oaie]|(?:si comunica|si conferma)\s+la revoca\s+(?:dello sciopero|dell[’']agitazione))\b`)
	revocationQualifier = regexp.MustCompile(`(?i)\b(?:non|se|qualora|eventuale|eventualmente|potrebbe|potrebbero|sarebbe|sarebbero)\b|\bin caso\b`)

	disruptionWords = []string{"cancellazion", "ritard", "riguarda", "riguarder", "interessat", "coinvolt", "possibil", "disagi", "sospension", "variazioni", "modifiche"}
	regularWords    = []string{"regolar", "non riguarda", "non coinvolt", "non interessat"}

	operatorSpecs = []struct {
		name    string
		aliases []string
	}{
		{"Arriva Udine", []string{"arriva udine", "area udinese"}},
		{"Trieste Trasporti", []string{"trieste trasporti", "area di trieste"}},
		{"APT Gorizia", []string{"apt gorizia", "area isontina"}},
		{"ATAP Pordenone", []string{"atap"}},
		{"Tpl Fvg", []string{"tpl fvg", "tplfvg"}},
	}
	coveredOperators = []string{"Arriva Udine", "Trieste Trasporti", "APT Gorizia"}
	cityOperators    = []struct {
		city     string
		operator string
	}{
		{"udine", "Arriva Udine"},
		{"trieste", "Trieste Trasporti"},
		{"muggia", "Trieste Trasporti"},
		{"monfalcone", "APT Gorizia"},
		{"gorizia", "APT Gorizia"},
		{"gradisca", "APT Gorizia"},
		{"grado", "APT Gorizia"},
	}
)

// Flow extracts TPL FVG strike notices.
type Flow struct{}

func (Flow) Version() int { return ExtractVersion }

// Extract refuses to run on the collection alone: TPL FVG publishes
// applicability only on each notice's page, so rfs must fetch the detail pages
// declared by DetailURLs before ExtractDetails can classify anything.
func (Flow) Extract(rfs.Page) ([]rfs.ExtractedItem, error) {
	return nil, errors.New("tplfvg: strike notices require their detail pages")
}

// DetailURLs returns the permalink of every notice that is still in force.
// Expired notices are the page's archive: they leave the observation without a
// cancellation claim.
func (Flow) DetailURLs(page rfs.Page) ([]string, error) {
	cards, err := parseCollection(page)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, card := range cards {
		if card.expired {
			continue
		}
		paths = append(paths, card.path)
	}
	return paths, nil
}

// ExtractDetails pairs each in-force notice with the detail page rfs fetched
// for it and emits one item per notice that establishes applicability to
// Arriva Udine, Trieste Trasporti, APT Gorizia or the consortium as a whole.
func (Flow) ExtractDetails(collection rfs.Page, details []rfs.Page) ([]rfs.ExtractedItem, error) {
	cards, err := parseCollection(collection)
	if err != nil {
		return nil, err
	}
	var active []noticeCard
	for _, card := range cards {
		if !card.expired {
			active = append(active, card)
		}
	}
	if len(active) != len(details) {
		return nil, fmt.Errorf("tplfvg: %d detail pages for %d in-force notices", len(details), len(active))
	}
	items := make([]rfs.ExtractedItem, 0, len(active))
	for i, card := range active {
		notice, err := parseNotice(details[i])
		if err != nil {
			return nil, fmt.Errorf("tplfvg: notice %s: %w", card.path, err)
		}
		scope, applicable := applicability(card.summary+"\n"+notice.title, notice.summary, notice.body)
		if !applicable {
			continue
		}
		status := statusActive
		if card.revoked || isRevoked(card.summary+"\n"+notice.title+"\n"+notice.summary+"\n"+notice.body) {
			status = statusRevoked
		}
		payload := noticePayload{
			Version:    payloadVersion,
			Status:     status,
			Scope:      scope,
			Label:      card.summary,
			DateLabel:  card.title,
			DateText:   notice.dateText,
			Hours:      notice.hours,
			Title:      notice.title,
			Summary:    notice.summary,
			Body:       notice.body,
			Guarantees: notice.guarantees,
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		link := canonicalURL(card.path)
		items = append(items, rfs.ExtractedItem{GUID: link, Link: link, Title: noticeTitle(titlePrefix(payload), payload), Description: string(data)})
	}
	return items, nil
}

// Changes emits one item per observed difference. A notice that disappears or
// expires is dropped without a cancellation claim, and a first observation
// announces the notices that are already in force but never a revoked one.
func (Flow) Changes(previous, current []rfs.ExtractedItem) ([]rfs.ExtractedItem, error) {
	stored := make(map[string]rfs.ExtractedItem, len(previous))
	for _, item := range previous {
		stored[item.GUID] = item
	}
	initial := len(previous) == 0
	var changes []rfs.ExtractedItem
	for _, item := range current {
		after, err := decodeNotice(item)
		if err != nil {
			return nil, err
		}
		before, seen := stored[item.GUID]
		delete(stored, item.GUID)
		if !seen {
			if initial && after.Status == statusRevoked {
				continue
			}
			changes = append(changes, changeItem(item, noticeTitle(titlePrefix(after), after), announcementDescription(after)))
			continue
		}
		beforePayload, beforeErr := decodeNotice(before)
		fields := changedFields(beforePayload, after)
		if beforeErr == nil && len(fields) == 0 {
			continue
		}
		prefix := "Aggiornato"
		if after.Status == statusRevoked {
			prefix = "Revocato"
		}
		changes = append(changes, changeItem(item, noticeTitle(prefix, after), updateDescription(after, fields)))
	}
	return changes, nil
}

// noticePayload is the versioned comparison state stored in
// ExtractedItem.Description. Upstream identity stays in the GUID; nothing
// mutable is part of it.
type noticePayload struct {
	Version    int      `json:"v"`
	Status     string   `json:"status"`
	Scope      []string `json:"scope"`
	Label      string   `json:"label"`
	DateLabel  string   `json:"date_label,omitempty"`
	DateText   string   `json:"date_text,omitempty"`
	Hours      []string `json:"hours,omitempty"`
	Title      string   `json:"title"`
	Summary    string   `json:"summary"`
	Body       string   `json:"body"`
	Guarantees string   `json:"guarantees,omitempty"`
}

type noticeCard struct {
	path    string
	title   string
	summary string
	expired bool
	revoked bool
}

type noticeDetail struct {
	title      string
	summary    string
	body       string
	guarantees string
	dateText   string
	hours      []string
}

func parseCollection(page rfs.Page) ([]noticeCard, error) {
	doc, err := rfs.ParseHTML(page)
	if err != nil {
		return nil, fmt.Errorf("tplfvg: parse collection: %w", err)
	}
	content := mainContent(doc)
	if content == nil {
		return nil, errors.New("tplfvg: collection page has no content")
	}
	if !looksLikeCollection(content) {
		return nil, errors.New("tplfvg: collection page not recognised")
	}
	var cards []noticeCard
	for _, anchor := range findAllByClass(content, "a", "card") {
		href := attr(anchor, "href")
		if !noticePathPattern.MatchString(href) {
			continue
		}
		expired, revoked, err := cardStatus(anchor)
		if err != nil {
			return nil, fmt.Errorf("tplfvg: notice %s: %w", href, err)
		}
		card := noticeCard{
			path:    href,
			title:   normalizeText(textOf(findFirstByClass(anchor, "h2", "card-title"))),
			summary: normalizeText(textOf(findFirstByClass(anchor, "div", "card-text"))),
			expired: expired,
			revoked: revoked,
		}
		if card.title == "" || card.summary == "" {
			return nil, fmt.Errorf("tplfvg: notice %s has no title or summary", href)
		}
		cards = append(cards, card)
	}
	return cards, nil
}

func parseNotice(page rfs.Page) (noticeDetail, error) {
	doc, err := rfs.ParseHTML(page)
	if err != nil {
		return noticeDetail{}, fmt.Errorf("parse notice: %w", err)
	}
	content := mainContent(doc)
	if content == nil {
		return noticeDetail{}, errors.New("notice page has no content")
	}
	notice := noticeDetail{
		title:   normalizeText(textOf(findFirst(content, "h1"))),
		summary: normalizeText(textOf(findFirstByClass(content, "div", "fs-3"))),
	}
	bodyRoot := findFirstByClass(content, "div", "pt-2")
	if bodyRoot == nil {
		bodyRoot = content
	}
	notice.body = noticeBody(bodyRoot)
	notice.guarantees = guaranteeText(bodyRoot)
	if notice.title == "" || notice.summary == "" || notice.body == "" {
		return noticeDetail{}, errors.New("notice page is missing its title, summary or body")
	}
	notice.hours = extractHours(notice.summary + "\n" + notice.body)
	text := notice.title + "\n" + notice.summary + "\n" + notice.body
	if match := dateTextPattern.FindStringSubmatch(text); match != nil {
		notice.dateText = normalizeText(match[1] + " " + match[2])
	}
	return notice, nil
}

// looksLikeCollection accepts the real page shapes: a notice list container or
// the page's own statement that no communications are published. Anything else
// is a malformed poll, not an empty collection.
func looksLikeCollection(content *html.Node) bool {
	if findFirstByClass(content, "div", "w-block-cards") != nil {
		return true
	}
	return strings.Contains(strings.ToLower(textOf(content)), "non ci sono comunicazioni")
}

func cardStatus(anchor *html.Node) (expired bool, revoked bool, err error) {
	image := findFirst(anchor, "img")
	if image == nil {
		return false, false, errors.New("notice card has no status image")
	}
	source := strings.ToLower(attr(image, "src"))
	switch {
	case strings.Contains(source, "valido"):
		return false, false, nil
	case strings.Contains(source, "scaduto"):
		return true, false, nil
	case strings.Contains(source, "revocat"):
		return false, true, nil
	default:
		return false, false, fmt.Errorf("unknown status image %q", attr(image, "src"))
	}
}

// noticeBody returns the announcement text, skipping the per-operator
// guarantee accordions that repeat context rather than announce the strike.
func noticeBody(root *html.Node) string {
	var parts []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && hasClass(n, "accordion") {
			return
		}
		if n.Type == html.ElementNode && hasClass(n, "richtext") {
			if text := textOf(n); text != "" {
				parts = append(parts, text)
			}
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return strings.Join(parts, "\n")
}

// guaranteeText renders each per-area accordion as "heading: content", which is
// how TPL FVG publishes guaranteed service windows for a strike.
func guaranteeText(root *html.Node) string {
	var parts []string
	for _, item := range findAllByClass(root, "div", "accordion-item") {
		heading := normalizeText(textOf(findFirst(item, "button")))
		body := noticeBody(item)
		switch {
		case heading != "" && body != "":
			parts = append(parts, heading+":\n"+body)
		case heading != "":
			parts = append(parts, heading)
		case body != "":
			parts = append(parts, body)
		}
	}
	return strings.Join(parts, "\n\n")
}

func extractHours(text string) []string {
	var hours []string
	seen := map[string]bool{}
	for _, match := range hoursPattern.FindAllStringSubmatch(text, -1) {
		span := match[1] + "-" + match[2]
		if seen[span] {
			continue
		}
		seen[span] = true
		hours = append(hours, span)
	}
	return hours
}

// isRevoked deliberately leaves ambiguous statements active. A conditional or
// negated sentence is not confirmation, even if it contains an affirmative phrase.
func isRevoked(text string) bool {
	for _, sentence := range strings.FieldsFunc(text, func(r rune) bool {
		return r == '.' || r == ';' || r == '!' || r == '?' || r == '\n'
	}) {
		if !revocationQualifier.MatchString(sentence) && revocationPattern.MatchString(sentence) {
			return true
		}
	}
	return false
}

// applicability reports which covered operators a notice affects. The
// operator's own summary decides: an operator named as running a regular
// service is never affected, and a notice whose only affected operator is
// outside the consortium (ATAP Pordenone) is not published. City names only
// count when no operator or consortium is named at all, so an ambiguous
// proclamation stays unpublished.
func applicability(title, summary, body string) ([]string, bool) {
	affected, regular := classify(title + "\n" + summary)
	bodyAffected, bodyRegular := classify(body)
	for operator := range bodyRegular {
		regular[operator] = true
	}
	if regular["Tpl Fvg"] {
		for _, operator := range coveredOperators {
			regular[operator] = true
		}
	}
	if len(affected) == 0 {
		affected = bodyAffected
	}
	if len(affected) > 0 {
		scope := coveredIntersection(affected, regular)
		if len(scope) == 0 {
			return nil, false
		}
		return scope, true
	}
	mentioned := operatorsIn(title + "\n" + summary)
	if len(mentioned) > 0 {
		scope := coveredIntersection(mentioned, regular)
		return scope, len(scope) > 0
	}
	cities := map[string]bool{}
	for _, operator := range cityScope(title + "\n" + summary) {
		cities[operator] = true
	}
	scope := coveredIntersection(cities, regular)
	return scope, len(scope) > 0
}

// classify attributes the operators named in each clause to the disrupted set
// or, when the clause describes regular service, to the regular set.
func classify(text string) (map[string]bool, map[string]bool) {
	affected := map[string]bool{}
	regular := map[string]bool{}
	for _, clause := range splitClauses(text) {
		lower := strings.ToLower(clause)
		if containsAny(lower, regularWords) {
			for name := range operatorsIn(clause) {
				regular[name] = true
			}
			continue
		}
		if containsAny(lower, disruptionWords) {
			for name := range operatorsIn(clause) {
				affected[name] = true
			}
		}
	}
	return affected, regular
}

// splitClauses breaks notice text into statements, so an operator named after
// "Regolari i servizi" is not attributed to the disruption sentence before it.
func splitClauses(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		return r == '.' || r == ';' || r == '\n' || r == '•' || r == '\r'
	})
}

func operatorsIn(text string) map[string]bool {
	found := map[string]bool{}
	lower := strings.ToLower(text)
	for _, spec := range operatorSpecs {
		for _, alias := range spec.aliases {
			if strings.Contains(lower, alias) {
				found[spec.name] = true
				break
			}
		}
	}
	return found
}

// coveredIntersection expands a consortium-wide scope to the covered operators
// that are not explicitly regular, and collapses a full consortium to "Tpl Fvg".
func coveredIntersection(affected, regular map[string]bool) []string {
	var scope []string
	for _, operator := range coveredOperators {
		if affected[operator] && !regular[operator] {
			scope = append(scope, operator)
		}
	}
	if affected["Tpl Fvg"] {
		for _, operator := range coveredOperators {
			if regular[operator] || affected[operator] {
				continue
			}
			scope = append(scope, operator)
		}
	}
	scope = canonicalScope(scope)
	if len(scope) == len(coveredOperators) {
		return []string{"Tpl Fvg"}
	}
	return scope
}

func canonicalScope(scope []string) []string {
	set := map[string]bool{}
	for _, operator := range scope {
		set[operator] = true
	}
	ordered := make([]string, 0, len(set))
	for _, operator := range coveredOperators {
		if set[operator] {
			ordered = append(ordered, operator)
		}
	}
	return ordered
}

func cityScope(text string) []string {
	lower := strings.ToLower(text)
	var scope []string
	for _, mapping := range cityOperators {
		if strings.Contains(lower, mapping.city) {
			scope = append(scope, mapping.operator)
		}
	}
	return canonicalScope(scope)
}

func containsAny(text string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func canonicalURL(path string) string {
	base, err := url.Parse(PageURL)
	if err != nil {
		return PageURL
	}
	resolved, err := base.Parse(path)
	if err != nil {
		return PageURL
	}
	return resolved.String()
}

func decodeNotice(item rfs.ExtractedItem) (noticePayload, error) {
	var payload noticePayload
	if err := json.Unmarshal([]byte(item.Description), &payload); err != nil {
		return noticePayload{}, fmt.Errorf("tplfvg: decode notice %s: %w", item.GUID, err)
	}
	if payload.Version != payloadVersion {
		return noticePayload{}, fmt.Errorf("tplfvg: notice %s has unsupported payload version %d", item.GUID, payload.Version)
	}
	return payload, nil
}

func changeItem(source rfs.ExtractedItem, title, description string) rfs.ExtractedItem {
	return rfs.ExtractedItem{GUID: source.GUID, Link: source.Link, Title: title, Description: description}
}

// titlePrefix keeps an already-revoked notice recognisable even when the feed
// first observes it in that state, and lets every later change keep the label.
func titlePrefix(payload noticePayload) string {
	if payload.Status == statusRevoked {
		return "Revocato"
	}
	return "Bus"
}

func noticeTitle(prefix string, payload noticePayload) string {
	label := payload.Label
	if label == "" {
		label = payload.Summary
	}
	if label == "" {
		label = payload.Title
	}
	tag := "Bus"
	if prefix != "Bus" {
		tag = prefix + " · Bus"
	}
	title := "[" + tag + " · " + scopeLabel(payload) + "] " + label
	if payload.DateLabel != "" {
		title += " — " + payload.DateLabel
	}
	if len(payload.Hours) > 0 {
		title += " (ore " + strings.Join(payload.Hours, ", ") + ")"
	}
	return title
}

func scopeLabel(payload noticePayload) string {
	if len(payload.Scope) == 0 {
		return "Tpl Fvg"
	}
	return strings.Join(payload.Scope, ", ")
}

func announcementDescription(payload noticePayload) string {
	var builder strings.Builder
	if payload.Status == statusRevoked {
		builder.WriteString("Avviso revocato dall'operatore; il testo che segue è l'ultimo pubblicato.\n\n")
	}
	writeField(&builder, "Stato", payload.Status)
	writeField(&builder, "Servizi potenzialmente interessati", strings.Join(payload.Scope, ", "))
	writeField(&builder, "Data", payload.DateLabel)
	writeField(&builder, "Orari", strings.Join(payload.Hours, ", "))
	writeField(&builder, "Titolo", payload.Title)
	blocks := []string{payload.Summary, payload.Body}
	if payload.Guarantees != "" {
		blocks = append(blocks, "Fasce di garanzia:\n"+payload.Guarantees)
	}
	for _, block := range blocks {
		if block == "" {
			continue
		}
		builder.WriteString("\n" + block + "\n")
	}
	return strings.TrimSpace(builder.String())
}

type fieldChange struct {
	label  string
	before string
	after  string
	text   bool
}

func changedFields(before, after noticePayload) []fieldChange {
	compare := []struct {
		label string
		text  bool
		get   func(noticePayload) string
	}{
		{"Stato", false, func(p noticePayload) string { return p.Status }},
		{"Servizi potenzialmente interessati", false, func(p noticePayload) string { return strings.Join(p.Scope, ", ") }},
		{"Data", false, func(p noticePayload) string { return p.DateLabel }},
		{"Data dell'agitazione", false, func(p noticePayload) string { return p.DateText }},
		{"Orari", false, func(p noticePayload) string { return strings.Join(p.Hours, ", ") }},
		{"Titolo", false, func(p noticePayload) string { return p.Title }},
		{"Etichetta", false, func(p noticePayload) string { return p.Label }},
		{"Riepilogo", true, func(p noticePayload) string { return p.Summary }},
		{"Testo", true, func(p noticePayload) string { return p.Body }},
		{"Fasce di garanzia", true, func(p noticePayload) string { return p.Guarantees }},
	}
	var fields []fieldChange
	for _, field := range compare {
		oldValue, newValue := field.get(before), field.get(after)
		if oldValue == newValue {
			continue
		}
		fields = append(fields, fieldChange{label: field.label, before: oldValue, after: newValue, text: field.text})
	}
	return fields
}

func updateDescription(after noticePayload, fields []fieldChange) string {
	var builder strings.Builder
	builder.WriteString("Aggiornamento dell'avviso: " + after.Summary)
	if after.DateLabel != "" {
		builder.WriteString(" — " + after.DateLabel)
	}
	builder.WriteString("\n")
	for _, field := range fields {
		if field.text {
			builder.WriteString("\n" + field.label + ":\n" + diffLines(field.before, field.after) + "\n")
			continue
		}
		builder.WriteString("\n" + field.label + ": " + displayValue(field.before) + " → " + displayValue(field.after))
	}
	return strings.TrimRight(builder.String(), "\n")
}

// diffLines reports the changed span of two texts, preserving line order and
// repetitions so a reworded notice stays readable.
func diffLines(before, after string) string {
	oldLines := splitLines(before)
	newLines := splitLines(after)
	start := 0
	for start < len(oldLines) && start < len(newLines) && oldLines[start] == newLines[start] {
		start++
	}
	oldEnd, newEnd := len(oldLines), len(newLines)
	for oldEnd > start && newEnd > start && oldLines[oldEnd-1] == newLines[newEnd-1] {
		oldEnd--
		newEnd--
	}
	var lines []string
	for _, line := range oldLines[start:oldEnd] {
		lines = append(lines, "- "+line)
	}
	for _, line := range newLines[start:newEnd] {
		lines = append(lines, "+ "+line)
	}
	if len(lines) == 0 {
		return "(nessuna differenza)"
	}
	return strings.Join(lines, "\n")
}

func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	return strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
}

func displayValue(value string) string {
	if value == "" {
		return "(assente)"
	}
	return value
}

func writeField(builder *strings.Builder, label, value string) {
	if value == "" {
		return
	}
	builder.WriteString(label + ": " + value + "\n")
}

func mainContent(doc *html.Node) *html.Node {
	if node := findFirstByAttr(doc, "id", "main-content"); node != nil {
		return node
	}
	return findFirst(doc, "body")
}

func findFirst(root *html.Node, tag string) *html.Node {
	if root == nil {
		return nil
	}
	var found *html.Node
	var walk func(*html.Node) bool
	walk = func(n *html.Node) bool {
		if n.Type == html.ElementNode && (tag == "" || n.Data == tag) {
			found = n
			return true
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			if walk(child) {
				return true
			}
		}
		return false
	}
	walk(root)
	return found
}

func findFirstByClass(root *html.Node, tag, class string) *html.Node {
	if root == nil {
		return nil
	}
	var found *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found != nil {
			return
		}
		if n.Type == html.ElementNode && n.Data == tag && hasClass(n, class) {
			found = n
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return found
}

func findAllByClass(root *html.Node, tag, class string) []*html.Node {
	if root == nil {
		return nil
	}
	var nodes []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == tag && hasClass(n, class) {
			nodes = append(nodes, n)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return nodes
}

func findFirstByAttr(root *html.Node, key, value string) *html.Node {
	if root == nil {
		return nil
	}
	var found *html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if found != nil {
			return
		}
		if n.Type == html.ElementNode && attr(n, key) == value {
			found = n
			return
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(root)
	return found
}

func hasClass(n *html.Node, class string) bool {
	for _, token := range strings.Fields(attr(n, "class")) {
		if token == class {
			return true
		}
	}
	return false
}

func attr(n *html.Node, key string) string {
	if n == nil {
		return ""
	}
	for _, attribute := range n.Attr {
		if attribute.Key == key {
			return attribute.Val
		}
	}
	return ""
}

// textOf renders an element's text with block boundaries as line breaks and
// collapses whitespace, so reformatting the same notice cannot change the
// comparison payload.
func textOf(root *html.Node) string {
	if root == nil {
		return ""
	}
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			// Collapse a text node's own whitespace, including source line
			// breaks, and only re-add the separation it actually carried.
			// Block boundaries, not source formatting, produce payload lines.
			data := n.Data
			core := strings.Join(strings.Fields(data), " ")
			if core == "" {
				builder.WriteString(" ")
				return
			}
			if unicode.IsSpace(rune(data[0])) && builder.Len() > 0 {
				builder.WriteString(" ")
			}
			builder.WriteString(core)
			if unicode.IsSpace(rune(data[len(data)-1])) {
				builder.WriteString(" ")
			}
			return
		case html.ElementNode:
			switch n.Data {
			case "script", "style", "noscript":
				return
			case "br":
				builder.WriteString("\n")
				return
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
		if n.Type == html.ElementNode && isBlockTag(n.Data) {
			builder.WriteString("\n")
		}
	}
	walk(root)
	return normalizeText(builder.String())
}

func isBlockTag(tag string) bool {
	switch tag {
	case "address", "article", "blockquote", "div", "dd", "dt", "fieldset", "figcaption", "figure", "footer", "form", "h1", "h2", "h3", "h4", "h5", "h6", "header", "hr", "li", "main", "nav", "ol", "p", "pre", "section", "table", "tbody", "td", "tfoot", "th", "thead", "tr", "ul":
		return true
	}
	return false
}

func normalizeText(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.Join(strings.Fields(line), " ")
		if line != "" {
			parts = append(parts, line)
		}
	}
	return tightenPunctuation(strings.Join(parts, "\n"))
}

// tightenPunctuation removes the spaces that re-serialized markup introduces
// between inline elements and punctuation, so a formatting-only upstream change
// does not read as a text change.
func tightenPunctuation(text string) string {
	for _, punct := range []string{".", ",", ";", ":", "!", "?", ")", "»"} {
		text = strings.ReplaceAll(text, " "+punct, punct)
	}
	return strings.ReplaceAll(text, "( ", "(")
}
