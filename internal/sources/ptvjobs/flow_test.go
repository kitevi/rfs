package ptvjobs_test

import (
	"strings"
	"testing"

	"github.com/ppowo/rfs/internal/sources/ptvjobs"
)

// listingPage is a trimmed capture of the PTV Personio career-site listing:
// job postings live in the RSC payload (self.__next_f.push chunks) as a
// "jobs" JSON array. 2713964/2713965/2713966 are Italy-remote; 2642353 has
// only a Perugia office; 2740615 is Germany-only.
const listingPage = `<!DOCTYPE html><html><head><title>Career Site</title></head><body>
<section class="jobs-group"></section>
<script>self.__next_f=self.__next_f||[];self.__next_f.push([1,"prefix"]);</script>
<script>self.__next_f.push([1,"{\"jobs\":[{\"id\":\"2713964\",\"name\":\"Customer Success Manager (m/f/d) - DACH\",\"main_office\":\"Remote\",\"additional_offices\":[\"Karlsruhe\",\"Remote - Italy\"],\"schedule\":\"Full-time\",\"department\":\"Customer Success\"},{\"id\":\"2713965\",\"name\":\"Support Engineer (m/f/d)\",\"main_office\":\"Remote - Italy\",\"additional_offices\":[],\"schedule\":\"Full-time\",\"department\":\"Support\"},{\"id\":\"2713966\",\"name\":\"QA Engineer (m/f/d)\",\"main_office\":\"remote - italy\",\"additional_offices\":[],\"schedule\":\"Full-time\",\"department\":\"Engineering\"},{\"id\":\"2642353\",\"name\":\"Accounting Specialist\",\"main_office\":\"Karlsruhe\",\"additional_offices\":[\"Perugia\",\"Oosterzele\"],\"schedule\":\"Full-time\",\"department\":\"Finance\"},{\"id\":\"2740615\",\"name\":\"IT System Administrator (m/f/d)\",\"main_office\":\"Karlsruhe\",\"additional_offices\":[],\"schedule\":\"Full-time\",\"department\":\"IT\"}]}"]);</script>
</body></html>`

func TestFlowExtractsRemoteItalyJobsAsItems(t *testing.T) {
	items, err := (ptvjobs.Flow{}).Extract([]byte(listingPage))
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 Remote-Italy items, got %d: %#v", len(items), items)
	}

	first := items[0]
	if first.GUID != "ptv-jobs:2713964" {
		t.Fatalf("unexpected first GUID: %q", first.GUID)
	}
	if first.Title != "Customer Success Manager (m/f/d) - DACH" {
		t.Fatalf("unexpected first title: %q", first.Title)
	}
	wantLink := "https://ptv-logistics.jobs.personio.com/job/2713964?language=en"
	if first.Link != wantLink {
		t.Fatalf("unexpected first link: %q, want %q", first.Link, wantLink)
	}
	if !strings.Contains(first.Description, "Remote - Italy") {
		t.Fatalf("expected description to mention Remote - Italy, got %q", first.Description)
	}
	if first.PubDate != nil {
		t.Fatalf("expected nil pubDate (listing carries no date), got %v", first.PubDate)
	}

	gotGUIDs := make(map[string]bool, len(items))
	for _, item := range items {
		gotGUIDs[item.GUID] = true
	}
	for _, want := range []string{"ptv-jobs:2713965", "ptv-jobs:2713966"} {
		if !gotGUIDs[want] {
			t.Fatalf("missing expected item %q in %v", want, gotGUIDs)
		}
	}
	for _, unwanted := range []string{"ptv-jobs:2642353", "ptv-jobs:2740615"} {
		if gotGUIDs[unwanted] {
			t.Fatalf("unexpected non-remote-Italy item %q emitted", unwanted)
		}
	}
}

func TestFlowRejectsEmptyJobsArray(t *testing.T) {
	page := `<!DOCTYPE html><html><body><script>self.__next_f.push([1,"{\"jobs\":[]}"]);</script></body></html>`
	if _, err := (ptvjobs.Flow{}).Extract([]byte(page)); err == nil {
		t.Fatal("expected error when the jobs array is empty, got nil")
	}
}

func TestFlowRejectsListingWithoutJobsArray(t *testing.T) {
	page := `<!DOCTYPE html><html><body><script>self.__next_f=self.__next_f||[];self.__next_f.push([1,"{\"other\":1}"]);</script></body></html>`
	if _, err := (ptvjobs.Flow{}).Extract([]byte(page)); err == nil {
		t.Fatal("expected error when page has no decodable jobs array, got nil")
	}
}

func TestFlowRejectsPageWithMalformedJobsJSON(t *testing.T) {
	page := `<!DOCTYPE html><html><body><script>self.__next_f.push([1,"{\"jobs\":[{not json"]);</script></body></html>`
	if _, err := (ptvjobs.Flow{}).Extract([]byte(page)); err == nil {
		t.Fatal("expected error for malformed jobs JSON, got nil")
	}
}

func TestFlowVersionMatchesExtractVersion(t *testing.T) {
	if (ptvjobs.Flow{}).Version() != ptvjobs.ExtractVersion {
		t.Fatalf("Flow.Version() = %d, want %d", (ptvjobs.Flow{}).Version(), ptvjobs.ExtractVersion)
	}
}
