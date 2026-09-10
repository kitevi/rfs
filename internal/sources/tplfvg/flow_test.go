package tplfvg

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ppowo/rfs/internal/rfs"
)

func fixture(t *testing.T, name string) rfs.Page {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return rfs.Page(data)
}

func extractItems(t *testing.T, indexName string, detailNames ...string) []rfs.ExtractedItem {
	t.Helper()
	index := fixture(t, indexName)
	urls, err := Flow{}.DetailURLs(index)
	if err != nil {
		t.Fatalf("DetailURLs(%s): %v", indexName, err)
	}
	if len(urls) != len(detailNames) {
		t.Fatalf("DetailURLs(%s) = %v, want %d active notices", indexName, urls, len(detailNames))
	}
	details := make([]rfs.Page, 0, len(detailNames))
	for _, name := range detailNames {
		details = append(details, fixture(t, name))
	}
	items, err := Flow{}.ExtractDetails(index, details)
	if err != nil {
		t.Fatalf("ExtractDetails(%s): %v", indexName, err)
	}
	return items
}

func payloadOf(t *testing.T, item rfs.ExtractedItem) noticePayload {
	t.Helper()
	var payload noticePayload
	if err := json.Unmarshal([]byte(item.Description), &payload); err != nil {
		t.Fatalf("decode payload %q: %v", item.Description, err)
	}
	if payload.Version != payloadVersion {
		t.Fatalf("payload version = %d, want %d", payload.Version, payloadVersion)
	}
	return payload
}

func TestChangesKeepsUnrevokedNoticesActive(t *testing.T) {
	for _, statement := range []string{
		"Lo sciopero non è stato revocato",
		"In caso di revoca dello sciopero sarà data comunicazione",
		"Lo sciopero potrebbe essere revocato",
		"Se lo sciopero è revocato sarà data comunicazione",
		"In caso di sciopero revocato sarà data comunicazione",
	} {
		t.Run(statement, func(t *testing.T) {
			page := string(fixture(t, "sciopero_udine_10set26_revoked.html"))
			old := "Lo sciopero è stato revocato dall’associazione sindacale proclamante."
			if !strings.Contains(page, old) {
				t.Fatal("fixture no longer contains revocation statement")
			}
			page = strings.Replace(page, old, statement+".", 1)
			current, err := (Flow{}).ExtractDetails(fixture(t, "scioperi_index.html"), []rfs.Page{rfs.Page(page)})
			if err != nil {
				t.Fatal(err)
			}
			for _, previous := range [][]rfs.ExtractedItem{nil, extractItems(t, "scioperi_index.html", "sciopero_udine_10set26.html")} {
				changes, err := (Flow{}).Changes(previous, current)
				if err != nil {
					t.Fatal(err)
				}
				if len(changes) != 1 {
					t.Fatalf("got %d announcements, want 1", len(changes))
				}
				if strings.HasPrefix(changes[0].Title, "[Revocato") {
					t.Fatalf("active strike labelled revoked: %s", changes[0].Title)
				}
			}
		})
	}
}

func TestRegularServicesStayExcludedAcrossFallbacks(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		wantItems  int
	}{
		{"city fallback", "Regolari i servizi di Arriva Udine.", 0},
		{"body fallback", "Possibili cancellazioni sui servizi di Arriva Udine.", 0},
		{"other affected operator", "Possibili cancellazioni sui servizi di Arriva Udine e Trieste Trasporti.", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page := rfs.Page(`<div id="main-content"><h1>Sciopero</h1><div class="fs-3">Regolari i servizi di Arriva Udine.</div><div class="pt-2"><div class="richtext">` + tc.body + `</div></div></div>`)
			current, err := (Flow{}).ExtractDetails(fixture(t, "scioperi_index.html"), []rfs.Page{page})
			if err != nil {
				t.Fatal(err)
			}
			changes, err := (Flow{}).Changes(nil, current)
			if err != nil {
				t.Fatal(err)
			}
			if len(changes) != tc.wantItems {
				t.Fatalf("got %d announcements, want %d: %#v", len(changes), tc.wantItems, changes)
			}
			if tc.wantItems == 1 && !strings.HasPrefix(changes[0].Title, "[Bus · Trieste Trasporti]") {
				t.Fatalf("regular operator must not be in affected scope: %s", changes[0].Title)
			}
		})
	}
}

func TestExtractRequiresDetailPages(t *testing.T) {
	if _, err := (Flow{}).Extract(fixture(t, "scioperi_index.html")); err == nil {
		t.Fatal("Extract must refuse a collection it cannot classify alone")
	}
}

func TestDetailURLsReturnsOnlyActiveNoticeCards(t *testing.T) {
	urls, err := (Flow{}).DetailURLs(fixture(t, "scioperi_index.html"))
	if err != nil {
		t.Fatalf("DetailURLs: %v", err)
	}
	want := []string{"/it/servizi/scioperi/10set26/"}
	if len(urls) != len(want) || urls[0] != want[0] {
		t.Fatalf("DetailURLs = %v, want %v", urls, want)
	}
}

func TestDetailURLsReturnsNothingForEmptyCollection(t *testing.T) {
	urls, err := (Flow{}).DetailURLs(fixture(t, "scioperi_index_empty.html"))
	if err != nil {
		t.Fatalf("DetailURLs: %v", err)
	}
	if len(urls) != 0 {
		t.Fatalf("DetailURLs = %v, want no active notices", urls)
	}
}

func TestDetailURLsRejectsMalformedCollections(t *testing.T) {
	index := string(fixture(t, "scioperi_index.html"))
	cases := map[string]rfs.Page{
		"error page":           rfs.Page("<html><body><h1>Page not found</h1></body></html>"),
		"empty body":           rfs.Page(""),
		"redesigned page":      rfs.Page("<html><body><div id=\"main-content\"><h1>Scioperi</h1><p>Elenco non disponibile.</p></div></body></html>"),
		"unknown status image": rfs.Page(strings.Replace(index, "avviso_valido.width-128.jpg", "avviso_nuovo.width-128.jpg", 1)),
		"card without summary": rfs.Page(strings.Replace(index, "<p data-block-key=\"csik0\">Udine, sciopero di 4 ore</p>", "<p data-block-key=\"csik0\"></p>", 1)),
	}
	for name, page := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := (Flow{}).DetailURLs(page); err == nil {
				t.Fatalf("DetailURLs accepted a %s", name)
			}
		})
	}
}

func TestExtractDetailsBuildsArrivaUdineNotice(t *testing.T) {
	items := extractItems(t, "scioperi_index.html", "sciopero_udine_10set26.html")
	if len(items) != 1 {
		t.Fatalf("extracted %d items, want 1", len(items))
	}
	item := items[0]
	wantGUID := "https://tplfvg.it/it/servizi/scioperi/10set26/"
	if item.GUID != wantGUID || item.Link != wantGUID {
		t.Fatalf("GUID/Link = %q/%q, want the canonical permalink %q", item.GUID, item.Link, wantGUID)
	}
	wantTitle := "[Bus · Arriva Udine] Udine, sciopero di 4 ore — 10 settembre 26 (ore 17:00-21:00, 17:45-21:45)"
	if item.Title != wantTitle {
		t.Fatalf("title = %q, want %q", item.Title, wantTitle)
	}
	if item.PubDate != nil {
		t.Fatalf("upstream supplies no publication date, got %v", item.PubDate)
	}
	if strings.ContainsAny(item.Description, "<>") {
		t.Fatalf("description carries HTML markup: %q", item.Description)
	}
	payload := payloadOf(t, item)
	if payload.Status != statusActive {
		t.Fatalf("status = %q, want %q", payload.Status, statusActive)
	}
	if len(payload.Scope) != 1 || payload.Scope[0] != "Arriva Udine" {
		t.Fatalf("scope = %v, want [Arriva Udine]", payload.Scope)
	}
	if payload.DateLabel != "10 settembre 26" || payload.DateText != "giovedì 10 settembre" {
		t.Fatalf("dates = %q/%q", payload.DateLabel, payload.DateText)
	}
	if len(payload.Hours) != 2 || payload.Hours[0] != "17:00-21:00" || payload.Hours[1] != "17:45-21:45" {
		t.Fatalf("hours = %v", payload.Hours)
	}
	if !strings.Contains(payload.Summary, "Possibili cancellazioni e ritardi") || !strings.Contains(payload.Summary, "Regolari") {
		t.Fatalf("summary = %q", payload.Summary)
	}
	if !strings.Contains(payload.Body, "servizio extraurbano") {
		t.Fatalf("body = %q", payload.Body)
	}
	if payload.Guarantees != "" {
		t.Fatalf("this notice has no per-operator guarantee section, got %q", payload.Guarantees)
	}
}

func TestExtractDetailsBuildsConsortiumNotice(t *testing.T) {
	items := extractItems(t, "scioperi_index_consortium.html", "sciopero_tplfvg_3ott25.html")
	if len(items) != 1 {
		t.Fatalf("extracted %d items, want 1", len(items))
	}
	payload := payloadOf(t, items[0])
	if len(payload.Scope) != 1 || payload.Scope[0] != "Tpl Fvg" {
		t.Fatalf("scope = %v, want consortium-wide [Tpl Fvg]", payload.Scope)
	}
	if !strings.HasPrefix(items[0].Title, "[Bus · Tpl Fvg] ") {
		t.Fatalf("title = %q, want a consortium scope label", items[0].Title)
	}
}

func TestExtractDetailsBuildsMultiOperatorNotice(t *testing.T) {
	items := extractItems(t, "scioperi_index_national.html", "sciopero_nazionale_18mag26.html")
	if len(items) != 1 {
		t.Fatalf("extracted %d items, want 1", len(items))
	}
	payload := payloadOf(t, items[0])
	want := []string{"Trieste Trasporti", "APT Gorizia"}
	if strings.Join(payload.Scope, ", ") != strings.Join(want, ", ") {
		t.Fatalf("scope = %v, want %v (Arriva Udine runs regular service)", payload.Scope, want)
	}
	if len(payload.Hours) != 0 {
		t.Fatalf("hours = %v, want none for a 24-hour strike", payload.Hours)
	}
	if !strings.Contains(payload.Guarantees, "Area udinese (Arriva Udine)") || !strings.Contains(payload.Guarantees, "Area isontina (APT Gorizia)") {
		t.Fatalf("guarantees = %q, want per-area sections", payload.Guarantees)
	}
	if strings.Contains(items[0].Title, "Arriva Udine") {
		t.Fatalf("title = %q, must not claim Arriva Udine is affected", items[0].Title)
	}
}

func TestExtractDetailsExcludesATAPOnlyNotice(t *testing.T) {
	items := extractItems(t, "scioperi_index_atap_only.html", "sciopero_udine_10set26_atap_only.html")
	if len(items) != 0 {
		t.Fatalf("extracted %#v, want ATAP-only notices excluded", items)
	}
}

func TestExtractDetailsMarksExplicitRevocation(t *testing.T) {
	items := extractItems(t, "scioperi_index.html", "sciopero_udine_10set26_revoked.html")
	if len(items) != 1 {
		t.Fatalf("extracted %d items, want 1", len(items))
	}
	payload := payloadOf(t, items[0])
	if payload.Status != statusRevoked {
		t.Fatalf("status = %q, want %q", payload.Status, statusRevoked)
	}
	if !strings.HasPrefix(items[0].Title, "[Revocato · Bus · Arriva Udine] ") {
		t.Fatalf("title = %q, want a revocation label", items[0].Title)
	}
}

func TestExtractDetailsHonoursRevocationBadge(t *testing.T) {
	// The collection card has its own revoked state. It is not present in the
	// live capture, so this pins the badge handling against the real markup.
	index := rfs.Page(strings.Replace(string(fixture(t, "scioperi_index.html")), "avviso_valido.width-128.jpg", "avviso_revocato.width-128.jpg", 1))
	items, err := (Flow{}).ExtractDetails(index, []rfs.Page{fixture(t, "sciopero_udine_10set26.html")})
	if err != nil {
		t.Fatalf("ExtractDetails: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("extracted %d items, want 1", len(items))
	}
	if payload := payloadOf(t, items[0]); payload.Status != statusRevoked {
		t.Fatalf("status = %q, want %q from the card badge", payload.Status, statusRevoked)
	}
	if !strings.HasPrefix(items[0].Title, "[Revocato · Bus · ") {
		t.Fatalf("title = %q, want a revocation label", items[0].Title)
	}
}

func TestExtractDetailsRequiresOneDetailPerActiveNotice(t *testing.T) {
	index := fixture(t, "scioperi_index.html")
	if _, err := (Flow{}).ExtractDetails(index, nil); err == nil {
		t.Fatal("missing detail pages must fail extraction")
	}
	details := []rfs.Page{fixture(t, "sciopero_udine_10set26.html"), fixture(t, "sciopero_udine_10set26.html")}
	if _, err := (Flow{}).ExtractDetails(index, details); err == nil {
		t.Fatal("extra detail pages must fail extraction")
	}
	broken := rfs.Page("<html><body><div id=\"main-content\"><p>avviso non disponibile</p></div></body></html>")
	if _, err := (Flow{}).ExtractDetails(index, []rfs.Page{broken}); err == nil {
		t.Fatal("a detail page without a notice must fail extraction")
	}
}

func TestChangesEmitsNothingForReformattedNotice(t *testing.T) {
	before := extractItems(t, "scioperi_index.html", "sciopero_udine_10set26.html")
	after := extractItems(t, "scioperi_index.html", "sciopero_udine_10set26_reformatted.html")
	changes, err := (Flow{}).Changes(before, after)
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("formatting-only poll emitted %#v", changes)
	}
}

func TestChangesEmitsUpdatedNotice(t *testing.T) {
	before := extractItems(t, "scioperi_index.html", "sciopero_udine_10set26.html")
	after := extractItems(t, "scioperi_index.html", "sciopero_udine_10set26_updated.html")
	changes, err := (Flow{}).Changes(before, after)
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("emitted %d changes, want 1", len(changes))
	}
	if changes[0].GUID != before[0].GUID {
		t.Fatalf("GUID = %q, want the unchanged entity identity %q", changes[0].GUID, before[0].GUID)
	}
	if !strings.HasPrefix(changes[0].Title, "[Aggiornato · Bus · Arriva Udine] ") {
		t.Fatalf("title = %q, want an update label", changes[0].Title)
	}
	if !strings.Contains(changes[0].Description, "17:00-21:00") || !strings.Contains(changes[0].Description, "18:00-21:00") {
		t.Fatalf("description = %q, want the changed hours", changes[0].Description)
	}
	if err := json.Unmarshal([]byte(changes[0].Description), &noticePayload{}); err == nil {
		t.Fatal("change descriptions must be human-readable, not the comparison payload")
	}
}

func TestChangesEmitsRevocationNotice(t *testing.T) {
	before := extractItems(t, "scioperi_index.html", "sciopero_udine_10set26.html")
	after := extractItems(t, "scioperi_index.html", "sciopero_udine_10set26_revoked.html")
	changes, err := (Flow{}).Changes(before, after)
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("emitted %d changes, want 1", len(changes))
	}
	if !strings.HasPrefix(changes[0].Title, "[Revocato · Bus · Arriva Udine] ") {
		t.Fatalf("title = %q, want a revocation label", changes[0].Title)
	}
	if !strings.Contains(strings.ToLower(changes[0].Description), "revocato") {
		t.Fatalf("description = %q, want the revocation wording", changes[0].Description)
	}
}

func TestChangesIgnoresDisappearance(t *testing.T) {
	before := extractItems(t, "scioperi_index.html", "sciopero_udine_10set26.html")
	changes, err := (Flow{}).Changes(before, nil)
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("expiry or removal emitted %#v, want nothing", changes)
	}
}

func TestChangesInitialObservationPublishesActiveNoticesOnly(t *testing.T) {
	active := extractItems(t, "scioperi_index.html", "sciopero_udine_10set26.html")
	revoked := extractItems(t, "scioperi_index.html", "sciopero_udine_10set26_revoked.html")
	changes, err := (Flow{}).Changes(nil, append(append([]rfs.ExtractedItem{}, active...), revoked...))
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if len(changes) != 1 || changes[0].GUID != active[0].GUID {
		t.Fatalf("initial observation emitted %#v, want only the active notice", changes)
	}
}

func TestApplicabilityRules(t *testing.T) {
	cases := []struct {
		name    string
		title   string
		summary string
		body    string
		want    string
	}{
		{
			name:    "single affected operator with regular operators named",
			summary: "Possibili cancellazioni e ritardi su tutta la rete urbana ed extraurbana di Arriva Udine. Regolari i servizi urbani ed extraurbani di APT Gorizia, ATAP Pordenone e Trieste Trasporti.",
			want:    "Arriva Udine",
		},
		{
			name:    "two affected operators",
			summary: "Possibili cancellazioni e ritardi sulle reti urbane ed extraurbane di Trieste Trasporti, APT Gorizia e ATAP Pordenone. Regolari i servizi di Arriva Udine.",
			want:    "Trieste Trasporti, APT Gorizia",
		},
		{
			name:    "consortium-wide scope",
			summary: "Possibili cancellazioni e ritardi su tutta la rete urbana ed extraurbana di Tpl Fvg.",
			want:    "Tpl Fvg",
		},
		{
			name:    "consortium scope with a regular operator",
			summary: "Possibili cancellazioni e ritardi su tutta la rete di Tpl Fvg. Regolari i servizi di Arriva Udine.",
			want:    "Trieste Trasporti, APT Gorizia",
		},
		{
			name:    "ATAP-only notice",
			summary: "Possibili cancellazioni e ritardi sulle reti urbane ed extraurbane di ATAP Pordenone. Regolari i servizi di Arriva Udine, Trieste Trasporti e APT Gorizia.",
			want:    "",
		},
		{
			name:    "city fallback when only a service area is named",
			title:   "Udine, sciopero di 24 ore",
			summary: "Sciopero di 24 ore.",
			want:    "Arriva Udine",
		},
		{
			name:    "generic proclamation without applicability",
			title:   "Sciopero nazionale di 24 ore",
			summary: "Sciopero proclamato dalle organizzazioni sindacali.",
			want:    "",
		},
		{
			name:    "body scope used when the summary is silent",
			title:   "Sciopero di 4 ore",
			summary: "Sciopero di 4 ore.",
			body:    "Lo sciopero riguarderà i servizi urbani ed extraurbani di Arriva Udine.",
			want:    "Arriva Udine",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scope, ok := applicability(tc.title, tc.summary, tc.body)
			if tc.want == "" {
				if ok {
					t.Fatalf("applicability = %v, want no applicability", scope)
				}
				return
			}
			if !ok || strings.Join(scope, ", ") != tc.want {
				t.Fatalf("applicability = %v (ok=%v), want %q", scope, ok, tc.want)
			}
		})
	}
}
