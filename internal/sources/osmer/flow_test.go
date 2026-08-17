package osmer_test

import (
	"strings"
	"testing"
	"time"

	"github.com/ppowo/rfs/internal/rfs"
	"github.com/ppowo/rfs/internal/sources/osmer"
)

const forecastPage = `<!doctype html>
<html><body>
<div class="panel panel-default">
  <div class="panel-heading">situazione generale</div>
  <div class="panel-body text-center pagination-centered">
    <div class="text-justify sipadd">Flusso atlantico instabile con possibili rovesci.</div>
  </div>
</div>
<div class="panel panel-primary">
  <div class="panel-heading">oggi</div>
  <div class="panel-body text-center pagination-centered">
    <div><strong>lunedì 17 agosto</strong></div>
    <div class="small">emissione: 16-08-2026 15:25 CEST</div>
    <div class="costa_bgrimg"><img alt="oggi" src="previ/costa_oggi_t.png?v=1786974607"></div>
    <div class="text-justify sipadd">Nuvolosit&agrave; variabile. Di notte e al mattino non &egrave; escluso qualche rovescio. Dal tardo pomeriggio saranno probabili rovesci e temporali sparsi.</div>
    <div class="table-responsive small">
      <table class="table table-striped nomargin"><tbody>
        <tr><td class="text-left">temperatura minima (&deg;C)</td><td>22/24</td></tr>
        <tr><td class="text-left">probabilit&agrave; di precipitazioni estese (%)</td><td>90</td></tr>
        <tr><td class="text-left">probabilit&agrave; di temporali (%)</td><td>90</td></tr>
      </tbody></table>
    </div>
  </div>
</div>
<div class="panel panel-primary">
  <div class="panel-heading">domani</div>
  <div class="panel-body text-center pagination-centered">
    <div><strong>martedì 18 agosto</strong></div>
    <div class="small">emissione: 17-08-2026 12:31 CEST</div>
    <div class="text-justify sipadd">Cielo in prevalenza sereno o poco nuvoloso con venti a regime di brezza.</div>
    <div class="table-responsive small">
      <table class="table table-striped nomargin"><tbody>
        <tr><td class="text-left">probabilit&agrave; di precipitazioni estese (%)</td><td>10</td></tr>
        <tr><td class="text-left">probabilit&agrave; di temporali (%)</td><td>10</td></tr>
      </tbody></table>
    </div>
  </div>
</div>
<div class="panel panel-primary">
  <div class="panel-heading">tendenza per <span class="hidden-xs">giovedì</span><span class="visible-xs">gio</span></div>
  <div class="panel-body text-center pagination-centered">
    <div><strong>giovedì 20 agosto</strong></div>
    <div class="small">emissione: 17-08-2026 11:45 CEST</div>
    <div class="text-justify sipadd">Cielo variabile con possibili temporali sparsi, specie dal pomeriggio.</div>
  </div>
</div>
</body></html>`

func TestFlowExtractsRainSectionsFromCoastBulletin(t *testing.T) {
	items, err := (osmer.Flow{}).Extract(rfs.Page(forecastPage))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}

	wantPubDates := []string{
		"2026-08-16T13:25:00Z",
		"2026-08-17T09:45:00Z",
	}
	wantGUIDs := []string{
		"osmer-rain-trieste:2026-08-17:20260816T1325Z",
		"osmer-rain-trieste:2026-08-20:20260817T0945Z",
	}
	for i := range items {
		if items[i].PubDate == nil || items[i].PubDate.Format(time.RFC3339) != wantPubDates[i] {
			t.Errorf("items[%d].PubDate = %v, want %s", i, items[i].PubDate, wantPubDates[i])
		}
		if items[i].GUID != wantGUIDs[i] {
			t.Errorf("items[%d].GUID = %q, want %q", i, items[i].GUID, wantGUIDs[i])
		}
		if items[i].Link != osmer.PageURL {
			t.Errorf("items[%d].Link = %q, want %q", i, items[i].Link, osmer.PageURL)
		}
	}
	if items[0].Title != "Rain near Trieste — lunedì 17 agosto" {
		t.Errorf("today title = %q", items[0].Title)
	}
	if !strings.Contains(items[0].Description, "rovesci e temporali sparsi") ||
		!strings.Contains(items[0].Description, "Probabilità di precipitazioni estese: 90%") ||
		!strings.Contains(items[0].Description, "Probabilità di temporali: 90%") ||
		strings.Contains(items[0].Description, "&agrave;") {
		t.Errorf("today description = %q", items[0].Description)
	}
	if items[1].Title != "Rain near Trieste — giovedì 20 agosto" {
		t.Errorf("trend title = %q", items[1].Title)
	}
	if strings.Contains(items[1].Description, "Probabilità") {
		t.Errorf("trend description unexpectedly includes probabilities: %q", items[1].Description)
	}
}

func TestFlowUsesCETAndRollsForecastYearAcrossNewYear(t *testing.T) {
	page := `<html><body><div class="panel-body text-center pagination-centered"><div><strong>sabato 2 gennaio</strong></div><div class="small">emissione: 31-12-2025 16:10 CET</div><div class="text-justify sipadd">Probabili piogge deboli.</div></div></body></html>`
	items, err := (osmer.Flow{}).Extract(rfs.Page(page))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].GUID != "osmer-rain-trieste:2026-01-02:20251231T1510Z" {
		t.Errorf("GUID = %q", items[0].GUID)
	}
	if items[0].PubDate == nil || items[0].PubDate.Format(time.RFC3339) != "2025-12-31T15:10:00Z" {
		t.Errorf("PubDate = %v", items[0].PubDate)
	}
}

func TestFlowErrorsWhenNoRainIsForecast(t *testing.T) {
	page := rfs.Page(`<html><body><div class="panel-body text-center pagination-centered"><div><strong>martedì 18 agosto</strong></div><div class="small">emissione: 17-08-2026 12:31 CEST</div><div class="text-justify sipadd">Cielo in prevalenza sereno o poco nuvoloso.</div></div></body></html>`)
	if _, err := (osmer.Flow{}).Extract(page); err == nil || !strings.Contains(err.Error(), "no rain-bearing") {
		t.Fatalf("error = %v", err)
	}
}

func TestFlowErrorsOnEmptyPage(t *testing.T) {
	if _, err := (osmer.Flow{}).Extract(nil); err == nil || !strings.Contains(err.Error(), "parse page") {
		t.Fatalf("error = %v", err)
	}
}

func TestFlowAssignsUniqueGuidsToSameIssueTimestamp(t *testing.T) {
	page := strings.Replace(forecastPage, "Cielo in prevalenza sereno o poco nuvoloso con venti a regime di brezza.", "Cielo variabile con possibili piogge.", 1)
	items, err := (osmer.Flow{}).Extract(rfs.Page(page))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if _, exists := seen[item.GUID]; exists {
			t.Fatalf("duplicate GUID %q from %+v", item.GUID, items)
		}
		seen[item.GUID] = struct{}{}
	}
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3", len(items))
	}
}

func TestPageURLTargetsTriesteCoastZone(t *testing.T) {
	const want = "https://www.meteo.fvg.it/previsioni.php?dettaglio=Z4&ln=it"
	if osmer.PageURL != want {
		t.Fatalf("PageURL = %q, want %q", osmer.PageURL, want)
	}
}

func TestFlowVersionMatchesExtractVersion(t *testing.T) {
	if (osmer.Flow{}).Version() != osmer.ExtractVersion {
		t.Fatalf("Flow.Version() = %d, want %d", (osmer.Flow{}).Version(), osmer.ExtractVersion)
	}
}
