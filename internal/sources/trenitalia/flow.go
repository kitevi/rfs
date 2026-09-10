// Package trenitalia watches Trenitalia's operator notices for
// disruption that reaches Friuli Venezia Giulia.
//
// Trenitalia publishes strike notices, real-time incident bulletins, per-region
// works pages and standing information pages on one Infomobilità page. Each
// notice carries an AEM component identifier in its markup, the regions it
// applies to, a publication date and its full body text, so a single page fetch
// is enough to identify and classify it.
package trenitalia

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"

	"github.com/ppowo/rfs/internal/rfs"
)

const (
	// PageURL is the Infomobilità notice page the Flow watches.
	PageURL = "https://www.trenitalia.com/it/informazioni/Infomobilita/notizie-infomobilita.html"
	// HumanURL is the page subscribers should open for context.
	HumanURL = PageURL

	// ExtractVersion is bumped whenever Extract or Changes output can change for
	// a fixed page.
	ExtractVersion = 2

	payloadVersion = 1
	statusActive   = "Attivo"
	statusRevoked  = "Revocato"
	// statusRestored marks a notice whose title states that an earlier
	// disruption is over. It is a terminal state: a first observation never
	// announces one, only the transition of a tracked disruption.
	statusRestored = "Ripristinato"

	scopeFVG      = "FVG"
	scopeNational = "Nazionale"

	fvgRegion = "friuli_venezia_giulia"
	// nationalRegionThreshold marks a notice tagged with most of Italy's regions
	// as national in scope even when its wording does not say so.
	nationalRegionThreshold = 15
	// maxNoticeLinks bounds the supporting links kept per notice.
	maxNoticeLinks = 10
)

var (
	// componentPattern extracts the AEM component name, which stays the same
	// while a notice is edited in place.
	componentPattern = regexp.MustCompile(`^(infomobility_summary_\d+)(?:-(?:title|category))?$`)

	// restoredPattern accepts a title that announces the end of a disruption. A
	// gradual resumption is not a restoration.
	restoredPattern = regexp.MustCompile(`(?i)\bcircolazione\s+(?:è\s+)?regolare\b|\bservizio\s+(?:è\s+)?regolare\b|\bregolarmente\b|\bripristinat\w*\b|\btornat\w*\s+alla\s+normalità\b`)

	// coveredRegionPattern names the covered region inside a standing page
	// title, which carries no region tag to classify it.
	coveredRegionPattern = regexp.MustCompile(`(?i)\bfriuli\b|\bvenezia\s*giulia\b|\bfvg\b`)

	// Accept affirmative status statements, not mentions of a possible revocation.
	revocationPattern   = regexp.MustCompile(`(?i)\b(?:revocat[oaie]\s+(?:(?:lo|il|la)\s+|l[’'])?(?:sciopero|agitazione)|(?:sciopero|agitazione)\s+(?:(?:è|sono|risulta|risultano)\s+(?:stat[oaie]\s+)?)?revocat[oaie]|(?:si comunica|si conferma)\s+la revoca\s+(?:dello sciopero|dell[’']agitazione))\b`)
	revocationQualifier = regexp.MustCompile(`(?i)\b(?:non|se|qualora|eventuale|eventualmente|potrebbe|potrebbero|sarebbe|sarebbero)\b|\bin caso\b`)

	// impactWords name an operational change to passenger services. A notice
	// whose own title names none of them is a standing information page, not a
	// disruption report.
	impactWords = []string{
		"agitazione sindacale", "allagament", "bus sostitutiv", "cancellat",
		"cancellazion", "circolazione", "condizioni meteo", "corse con bus",
		"deviazion", "graduale ripresa", "guasto", "inconveniente", "incendio",
		"interrott", "interruzion", "lavori", "limitazion", "maltempo",
		"modifiche al servizio", "rallentament", "regolare", "ripristin",
		"ritard", "sciopero", "servizio sospeso", "sospension", "sospes",
		"variazione", "variazion",
	}
	// standingPagePrefixes name notices that are per-region or national
	// information pages rather than one event bulletin.
	standingPagePrefixes = []string{"INFORMAZIONI", "INFOLAVORI", "INFOTRENI"}

	freightWords          = []string{"merci", "cargo", "merciario"}
	passengerOnlyWords    = []string{"viaggiatori", "passeggeri", "treni regionali", "servizio regionale", "servizi regionali", "trasporto regionale", "intercity", "frecce", "lunga percorrenza", "treni garantiti", "servizi garantiti"}
	passengerServiceWords = append([]string{"treni", "treno", "regionale", "regionali", "circolazione", "servizi", "corse", "collegamenti"}, passengerOnlyWords...)
	nationalScopeWords    = []string{"sciopero nazionale", "sciopero generale nazionale", "sciopero nazionale generale"}
	nationalOperatorWords = []string{"trenitalia", "gruppo fs", "fs group"}
	// coveredRouteWords name the FVG cities the specification covers: a notice
	// about a service through one of them affects travel in the region even when
	// upstream tags it with a neighbouring region only.
	coveredRouteWords = []string{"udine", "trieste", "gorizia", "monfalcone"}
)

// Flow extracts Trenitalia notices that affect travel in FVG.
type Flow struct{}

func (Flow) Version() int { return ExtractVersion }

func (Flow) Extract(page rfs.Page) ([]rfs.ExtractedItem, error) {
	notices, err := parseNotices(page)
	if err != nil {
		return nil, err
	}
	var items []rfs.ExtractedItem
	for _, notice := range notices {
		if !isDisruptionNotice(notice.title) {
			continue
		}
		scope, applicable := classification(notice.title, notice.body, notice.regions)
		if !applicable {
			continue
		}
		status := statusActive
		switch {
		case isRevoked(notice.title + "\n" + notice.body):
			status = statusRevoked
		case isRestored(notice.title):
			status = statusRestored
		}
		payload := noticePayload{
			Version:    payloadVersion,
			Status:     status,
			Scope:      scope,
			Regions:    notice.regions,
			NoticeDate: notice.noticeDate,
			Title:      notice.title,
			Body:       notice.body,
			Links:      notice.links,
		}
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		link := HumanURL + "#" + notice.id
		items = append(items, rfs.ExtractedItem{GUID: link, Link: link, Title: noticeTitle(titlePrefix(payload), payload), Description: string(data)})
	}
	return items, nil
}

// Changes emits one item per observed difference. A notice that disappears or
// expires is dropped without a cancellation claim. A disruption that is already
// over when it is first observed — revoked or restored — is history rather than
// news, so only a disruption tracked while it was active can announce its end.
func (Flow) Changes(previous, current []rfs.ExtractedItem) ([]rfs.ExtractedItem, error) {
	stored := make(map[string]rfs.ExtractedItem, len(previous))
	for _, item := range previous {
		stored[item.GUID] = item
	}
	var changes []rfs.ExtractedItem
	for _, item := range current {
		after, err := decodeNotice(item)
		if err != nil {
			return nil, err
		}
		before, seen := stored[item.GUID]
		delete(stored, item.GUID)
		if !seen {
			// A terminal notice is history, not news: only a disruption
			// observed while it was active can announce its own end.
			if after.Status == statusRevoked || after.Status == statusRestored {
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
		switch after.Status {
		case statusRevoked:
			prefix = "Revocato"
		case statusRestored:
			prefix = "Ripristinato"
		}
		changes = append(changes, changeItem(item, noticeTitle(prefix, after), updateDescription(after, fields)))
	}
	return changes, nil
}

// noticePayload is the versioned comparison state stored in
// ExtractedItem.Description. The upstream component id stays in the GUID, so no
// mutable field is part of identity.
type noticePayload struct {
	Version    int      `json:"v"`
	Status     string   `json:"status"`
	Scope      string   `json:"scope"`
	Regions    []string `json:"regions,omitempty"`
	NoticeDate string   `json:"notice_date,omitempty"`
	Title      string   `json:"title"`
	Body       string   `json:"body"`
	Links      []string `json:"links,omitempty"`
}

type noticeSection struct {
	id         string
	regions    []string
	title      string
	noticeDate string
	body       string
	links      []string
}

func parseNotices(page rfs.Page) ([]noticeSection, error) {
	doc, err := rfs.ParseHTML(page)
	if err != nil {
		return nil, fmt.Errorf("trenitalia: parse page: %w", err)
	}
	root := findFirstByClass(doc, "div", "infomobility-list")
	if root == nil {
		root = findFirst(doc, "body")
	}
	if root == nil {
		return nil, errors.New("trenitalia: page has no content")
	}
	sections := findAllByClass(root, "div", "infomobility")
	if len(sections) == 0 {
		return nil, errors.New("trenitalia: no notice sections found")
	}
	notices := make([]noticeSection, 0, len(sections))
	for _, section := range sections {
		notice := noticeSection{
			regions:    sectionRegions(section),
			title:      normalizeText(textOf(findFirstByClass(section, "h3", "infomobility-title"))),
			noticeDate: normalizeText(textOf(findFirstByClass(section, "p", "infomobility-date"))),
		}
		notice.id = componentID(section)
		bodyRoot := findFirstByClass(section, "div", "description")
		if bodyRoot == nil {
			bodyRoot = section
		}
		notice.body = normalizeText(textOf(bodyRoot))
		notice.links = bodyLinks(bodyRoot)
		if notice.title != "" && notice.id == "" {
			return nil, fmt.Errorf("trenitalia: notice %q has no upstream component id", notice.title)
		}
		notices = append(notices, notice)
	}
	return notices, nil
}

func componentID(section *html.Node) string {
	var id string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if id != "" {
			return
		}
		if n.Type == html.ElementNode {
			if match := componentPattern.FindStringSubmatch(attr(n, "id")); match != nil {
				id = match[1]
				return
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(section)
	return id
}

func sectionRegions(section *html.Node) []string {
	item := findFirstByClass(section, "div", "accordion-item")
	if item == nil {
		return nil
	}
	var regions []string
	for _, region := range strings.Split(attr(item, "data-region"), ",") {
		region = strings.TrimSpace(region)
		if region == "" || region == "empty" {
			continue
		}
		regions = append(regions, region)
	}
	return regions
}

func bodyLinks(body *html.Node) []string {
	base, err := url.Parse(PageURL)
	if err != nil {
		return nil
	}
	var links []string
	seen := map[string]bool{}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			href := strings.TrimSpace(attr(n, "href"))
			if href != "" && !strings.HasPrefix(href, "#") && !strings.HasPrefix(href, "mailto:") {
				if resolved, err := base.Parse(href); err == nil && (resolved.Scheme == "http" || resolved.Scheme == "https") {
					link := resolved.String()
					if !seen[link] && len(links) < maxNoticeLinks {
						seen[link] = true
						links = append(links, link)
					}
				}
			}
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(body)
	return links
}

// isDisruptionNotice accepts a notice whose own title reports an operational
// change to train services: a strike, an incident bulletin, or a region's works
// page. The national high-speed delay list, the regional information index and
// other standing pages name no impact in their titles and stay out of the feed.
func isDisruptionNotice(title string) bool {
	return containsAny(strings.ToLower(title), impactWords)
}

// isRestored reports whether the operator's own title announces the end of a
// disruption. Text buried in an accumulated body cannot override the title, so
// a stale "servizio regolare" line in an old update stays inert.
func isRestored(title string) bool {
	return restoredPattern.MatchString(title)
}

// standingPage reports whether a notice is a per-region or national information
// page rather than one event bulletin.
func standingPage(title string) bool {
	upper := strings.ToUpper(strings.TrimSpace(title))
	for _, prefix := range standingPagePrefixes {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
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

// classification reports the scope a notice covers. A notice tagged with
// Friuli Venezia Giulia is local; an untagged notice only qualifies when its
// own text establishes a national strike by Trenitalia/FS or writes about a
// covered route. A standing page lists sections for several places, so only its
// title can establish coverage: a body mention of an FVG line does not turn the
// national information index into an FVG notice. Notices limited to other
// regions, to freight, or to a personnel category with no passenger service
// impact are rejected, and an ambiguous proclamation stays unpublished.
func classification(title, body string, regions []string) (string, bool) {
	text := strings.ToLower(title + "\n" + body)
	if !hasPassengerService(text) {
		return "", false
	}
	if containsAny(text, freightWords) && !containsAny(text, passengerOnlyWords) {
		return "", false
	}
	local := containsRegion(regions, fvgRegion)
	var route bool
	if standingPage(title) {
		local = local || coveredRegionPattern.MatchString(title)
		route = containsAny(strings.ToLower(title), coveredRouteWords)
	} else {
		route = containsAny(text, coveredRouteWords)
	}
	national := containsAny(text, nationalScopeWords) && containsAny(text, nationalOperatorWords)
	switch {
	case (local || route) && (national || len(regions) >= nationalRegionThreshold):
		return scopeNational, true
	case local || route:
		return scopeFVG, true
	case len(regions) == 0 && national:
		return scopeNational, true
	default:
		return "", false
	}
}

func containsRegion(regions []string, want string) bool {
	for _, region := range regions {
		if region == want {
			return true
		}
	}
	return false
}

func hasPassengerService(text string) bool {
	return containsAny(text, passengerServiceWords)
}

func containsAny(text string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func decodeNotice(item rfs.ExtractedItem) (noticePayload, error) {
	var payload noticePayload
	if err := json.Unmarshal([]byte(item.Description), &payload); err != nil {
		return noticePayload{}, fmt.Errorf("trenitalia: decode notice %s: %w", item.GUID, err)
	}
	if payload.Version != payloadVersion {
		return noticePayload{}, fmt.Errorf("trenitalia: notice %s has unsupported payload version %d", item.GUID, payload.Version)
	}
	return payload, nil
}

func changeItem(source rfs.ExtractedItem, title, description string) rfs.ExtractedItem {
	return rfs.ExtractedItem{GUID: source.GUID, Link: source.Link, Title: title, Description: description}
}

func titlePrefix(payload noticePayload) string {
	switch payload.Status {
	case statusRevoked:
		return "Revocato"
	case statusRestored:
		return "Ripristinato"
	}
	return "Treni"
}

func noticeTitle(prefix string, payload noticePayload) string {
	tag := "Treni"
	if prefix != "Treni" {
		tag = prefix + " · Treni"
	}
	return "[" + tag + " · " + payload.Scope + "] " + payload.Title
}

func announcementDescription(payload noticePayload) string {
	var builder strings.Builder
	switch payload.Status {
	case statusRevoked:
		builder.WriteString("Avviso revocato dall'operatore; il testo che segue è l'ultimo pubblicato.\n\n")
	case statusRestored:
		builder.WriteString("L'operatore dichiara concluso il disagio; il testo che segue è l'ultimo pubblicato.\n\n")
	}
	writeField(&builder, "Stato", payload.Status)
	writeField(&builder, "Ambito", payload.Scope)
	writeField(&builder, "Regioni", strings.Join(payload.Regions, ", "))
	writeField(&builder, "Data dell'avviso", payload.NoticeDate)
	writeField(&builder, "Titolo", payload.Title)
	if payload.Body != "" {
		builder.WriteString("\n" + payload.Body + "\n")
	}
	if len(payload.Links) > 0 {
		builder.WriteString("\nLink:\n")
		for _, link := range payload.Links {
			builder.WriteString("- " + link + "\n")
		}
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
		{"Ambito", false, func(p noticePayload) string { return p.Scope }},
		{"Regioni", false, func(p noticePayload) string { return strings.Join(p.Regions, ", ") }},
		{"Titolo", false, func(p noticePayload) string { return p.Title }},
		{"Testo", true, func(p noticePayload) string { return p.Body }},
		{"Link", false, func(p noticePayload) string { return strings.Join(p.Links, "\n") }},
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
	builder.WriteString("Aggiornamento dell'avviso: " + after.Title)
	if after.NoticeDate != "" {
		builder.WriteString(" (" + after.NoticeDate + ")")
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
// collapses whitespace inside each text node, so source formatting cannot
// change the comparison payload.
func textOf(root *html.Node) string {
	if root == nil {
		return ""
	}
	var builder strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			data := n.Data
			core := strings.Join(strings.Fields(data), " ")
			if core == "" {
				builder.WriteString(" ")
				return
			}
			if len(data) > 0 && isSpaceByte(data[0]) && builder.Len() > 0 {
				builder.WriteString(" ")
			}
			builder.WriteString(core)
			if len(data) > 0 && isSpaceByte(data[len(data)-1]) {
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

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\v' || b == '\f'
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
