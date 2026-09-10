package trenitaliascioperi

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

func extract(t *testing.T, name string) []rfs.ExtractedItem {
	t.Helper()
	items, err := (Flow{}).Extract(fixture(t, name))
	if err != nil {
		t.Fatalf("Extract(%s): %v", name, err)
	}
	return items
}

func payloadOf(t *testing.T, item rfs.ExtractedItem) noticePayload {
	t.Helper()
	var payload noticePayload
	if err := json.Unmarshal([]byte(item.Description), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
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
			page := string(fixture(t, "notizie_national_revoked.html"))
			old := "Lo sciopero è stato revocato dall’organizzazione sindacale proclamante."
			if !strings.Contains(page, old) {
				t.Fatal("fixture no longer contains revocation statement")
			}
			page = strings.Replace(page, old, statement+".", 1)
			current, err := (Flow{}).Extract(rfs.Page(page))
			if err != nil {
				t.Fatal(err)
			}
			for _, previous := range [][]rfs.ExtractedItem{nil, extract(t, "notizie_20260905.html")} {
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

func TestExtractPublishesOnlyApplicableStrikeNotices(t *testing.T) {
	items := extract(t, "notizie_20260905.html")
	if len(items) != 1 {
		t.Fatalf("extracted %d notices, want only the national strike: %#v", len(items), items)
	}
	item := items[0]
	wantGUID := HumanURL + "#infomobility_summary_548526369"
	if item.GUID != wantGUID || item.Link != wantGUID {
		t.Fatalf("GUID/Link = %q/%q, want %q", item.GUID, item.Link, wantGUID)
	}
	if !strings.HasPrefix(item.Title, "[Treni · Nazionale] ") {
		t.Fatalf("title = %q, want a national scope label", item.Title)
	}
	if !strings.Contains(item.Title, "sciopero nazionale del personale del Gruppo FS") {
		t.Fatalf("title = %q, want the notice title", item.Title)
	}
	payload := payloadOf(t, item)
	if payload.Status != statusActive || payload.Scope != scopeNational {
		t.Fatalf("status/scope = %q/%q", payload.Status, payload.Scope)
	}
	if payload.NoticeDate != "05/09/2026" {
		t.Fatalf("notice date = %q, want the upstream date", payload.NoticeDate)
	}
	if !strings.Contains(strings.Join(payload.Regions, ","), "friuli_venezia_giulia") {
		t.Fatalf("regions = %v, want the upstream region tags", payload.Regions)
	}
	if !strings.Contains(payload.Body, "I treni possono subire cancellazioni o variazioni.") {
		t.Fatalf("body = %q, want the notice text", payload.Body)
	}
	if strings.ContainsAny(payload.Body, "<>") {
		t.Fatalf("body carries markup: %q", payload.Body)
	}
	if len(payload.Links) == 0 {
		t.Fatal("no supporting links extracted")
	}
	var official bool
	for _, link := range payload.Links {
		if !strings.HasPrefix(link, "https://") {
			t.Fatalf("link %q is not HTTPS", link)
		}
		if strings.Contains(link, "trenitalia.com") {
			official = true
		}
	}
	if !official {
		t.Fatalf("links = %v, want an official trenitalia.com link", payload.Links)
	}
}

func TestExtractPublishesNothingWhenNoStrikeIsPublished(t *testing.T) {
	items := extract(t, "notizie_20260910.html")
	if len(items) != 0 {
		t.Fatalf("extracted %#v from a page without strike notices", items)
	}
}

func TestExtractPublishesUntaggedNationalNotice(t *testing.T) {
	items := extract(t, "notizie_national_untagged.html")
	if len(items) != 1 {
		t.Fatalf("extracted %d notices, want the untagged national strike", len(items))
	}
	payload := payloadOf(t, items[0])
	if payload.Scope != scopeNational {
		t.Fatalf("scope = %q, want %q for a national notice without region tags", payload.Scope, scopeNational)
	}
	if len(payload.Regions) != 0 {
		t.Fatalf("regions = %v, want none", payload.Regions)
	}
}

func TestExtractExcludesFreightOnlyNotice(t *testing.T) {
	items := extract(t, "notizie_national_freight.html")
	if len(items) != 0 {
		t.Fatalf("extracted %#v, want freight-only notices excluded", items)
	}
}

func TestExtractMarksExplicitRevocation(t *testing.T) {
	items := extract(t, "notizie_national_revoked.html")
	if len(items) != 1 {
		t.Fatalf("extracted %d notices, want 1", len(items))
	}
	payload := payloadOf(t, items[0])
	if payload.Status != statusRevoked {
		t.Fatalf("status = %q, want %q", payload.Status, statusRevoked)
	}
	if !strings.HasPrefix(items[0].Title, "[Revocato · Treni · Nazionale] ") {
		t.Fatalf("title = %q, want a revocation label", items[0].Title)
	}
}

func TestExtractRejectsMalformedPages(t *testing.T) {
	cases := map[string]rfs.Page{
		"error page": rfs.Page("<html><body><h1>Page not found</h1></body></html>"),
		"empty body": rfs.Page(""),
	}
	for name, page := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := (Flow{}).Extract(page); err == nil {
				t.Fatalf("Extract accepted a %s", name)
			}
		})
	}
}

func TestClassificationRules(t *testing.T) {
	national := "Dalle ore 21:18 è indetto uno sciopero nazionale del personale del Gruppo FS, Trenitalia. I treni possono subire cancellazioni o variazioni."
	cases := []struct {
		name    string
		title   string
		body    string
		regions []string
		want    string
	}{
		{"FVG region tag", "Sciopero del personale di Trenitalia", "I treni regionali possono subire variazioni.", []string{"friuli_venezia_giulia", "veneto"}, scopeFVG},
		{"unrelated region", "Sciopero del personale di Trenitalia", "I treni regionali possono subire variazioni.", []string{"trentino", "alto_adige"}, ""},
		{"national scope without region tags", "Sciopero nazionale del personale del Gruppo FS", national, nil, scopeNational},
		{"generic proclamation", "Sciopero", "Sciopero proclamato dalle organizzazioni sindacali.", nil, ""},
		{"freight only", "Sciopero del settore merci", "I treni merci possono subire variazioni.", []string{"friuli_venezia_giulia"}, ""},
		{"no passenger service impact", "Sciopero del personale di manutenzione", "Sciopero degli impianti di manutenzione.", []string{"friuli_venezia_giulia"}, ""},
		{"passenger strike in FVG without the national label", "Sciopero del personale di Trenitalia", "Per i treni regionali in Friuli Venezia Giulia possono verificarsi cancellazioni o variazioni.", []string{"friuli_venezia_giulia"}, scopeFVG},
		{"covered route tagged with another region", "Sciopero del personale di Trenitalia", "I treni regionali della linea Venezia - Trieste possono subire variazioni.", []string{"veneto"}, scopeFVG},
		{"proclamato with established applicability", "Sciopero proclamato del personale di Trenitalia", "I treni regionali in Friuli Venezia Giulia possono subire cancellazioni.", []string{"friuli_venezia_giulia"}, scopeFVG},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scope, ok := classification(tc.title, tc.body, tc.regions)
			if tc.want == "" {
				if ok {
					t.Fatalf("classification = %q, want no applicability", scope)
				}
				return
			}
			if !ok || scope != tc.want {
				t.Fatalf("classification = %q (ok=%v), want %q", scope, ok, tc.want)
			}
		})
	}
}

func TestChangesIgnoreNoticeDateOnlyChange(t *testing.T) {
	before := extract(t, "notizie_20260905.html")
	after := extract(t, "notizie_20260905.html")
	payload := payloadOf(t, after[0])
	payload.NoticeDate = "06/09/2026"
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	after[0].Description = string(data)
	changes, err := (Flow{}).Changes(before, after)
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("an upstream update timestamp alone emitted %#v", changes)
	}
}

func TestChangesEmitsUpdatedInterval(t *testing.T) {
	before := extract(t, "notizie_20260905.html")
	after := extract(t, "notizie_national_updated.html")
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
	if !strings.HasPrefix(changes[0].Title, "[Aggiornato · Treni · Nazionale] ") {
		t.Fatalf("title = %q, want an update label", changes[0].Title)
	}
	if !strings.Contains(changes[0].Description, "21:18") || !strings.Contains(changes[0].Description, "22:18") {
		t.Fatalf("description = %q, want the changed interval", changes[0].Description)
	}
}

func TestChangesEmitsRevocationNotice(t *testing.T) {
	before := extract(t, "notizie_20260905.html")
	after := extract(t, "notizie_national_revoked.html")
	changes, err := (Flow{}).Changes(before, after)
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("emitted %d changes, want 1", len(changes))
	}
	if !strings.HasPrefix(changes[0].Title, "[Revocato · Treni · Nazionale] ") {
		t.Fatalf("title = %q, want a revocation label", changes[0].Title)
	}
	if !strings.Contains(strings.ToLower(changes[0].Description), "revocato") {
		t.Fatalf("description = %q, want the revocation wording", changes[0].Description)
	}
}

func TestChangesIgnoreFormattingOnlyPoll(t *testing.T) {
	before := extract(t, "notizie_national_updated.html")
	separator := ">" + "\n" + "  <"
	formatted := strings.ReplaceAll(string(fixture(t, "notizie_national_updated.html")), "><", separator)
	after, err := (Flow{}).Extract(rfs.Page(formatted))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("reformatting changed the notice count: %d -> %d", len(before), len(after))
	}
	changes, err := (Flow{}).Changes(before, after)
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("formatting-only poll emitted %#v", changes)
	}
}

func TestExtractDropsUnsafeSupportingLinks(t *testing.T) {
	page := string(fixture(t, "notizie_20260905.html"))
	page = strings.Replace(page, "https://www.trenitalia.com/it/informazioni/treni-garantiti-incasodisciopero.html", "javascript:alert(1)", 1)
	items, err := (Flow{}).Extract(rfs.Page(page))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("extracted %d notices, want 1", len(items))
	}
	payload := payloadOf(t, items[0])
	for _, link := range payload.Links {
		if !strings.HasPrefix(link, "https://") && !strings.HasPrefix(link, "http://") {
			t.Fatalf("link %q is not a safe HTTP(S) URL", link)
		}
	}
	if len(payload.Links) == 0 {
		t.Fatal("the remaining official link was dropped")
	}
}

func TestChangesIgnoresDisappearance(t *testing.T) {
	before := extract(t, "notizie_20260905.html")
	changes, err := (Flow{}).Changes(before, nil)
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("a withdrawn notice emitted %#v, want nothing", changes)
	}
}

func TestChangesInitialObservationPublishesActiveNoticesOnly(t *testing.T) {
	active := extract(t, "notizie_20260905.html")
	revoked := extract(t, "notizie_national_revoked.html")
	changes, err := (Flow{}).Changes(nil, append(append([]rfs.ExtractedItem{}, active...), revoked...))
	if err != nil {
		t.Fatalf("Changes: %v", err)
	}
	if len(changes) != 1 || changes[0].GUID != active[0].GUID {
		t.Fatalf("initial observation emitted %#v, want only the active notice", changes)
	}
}
