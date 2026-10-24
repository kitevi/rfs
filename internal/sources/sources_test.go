package sources_test

import (
	"strings"
	"testing"

	"github.com/kitevi/rfs/internal/rfs"
	"github.com/kitevi/rfs/internal/sources"
	"github.com/kitevi/rfs/internal/sources/catalog"
	"github.com/kitevi/rfs/internal/sources/cittafiera"
	"github.com/kitevi/rfs/internal/sources/coached"
	"github.com/kitevi/rfs/internal/sources/malstack"
	"github.com/kitevi/rfs/internal/sources/onepiece"
	"github.com/kitevi/rfs/internal/sources/visionario"
)

func TestAllIncludesPTGSource(t *testing.T) {
	var found bool
	for _, source := range sources.All() {
		if source.ID != "ptg" {
			continue
		}
		found = true
		if source.URL != catalog.PTG.PageURL() {
			t.Fatalf("ptg source URL = %q, want %q", source.URL, catalog.PTG.PageURL())
		}
		if source.Meta.Title != "Private Trackers General" {
			t.Fatalf("ptg source title = %q", source.Meta.Title)
		}
		if source.Meta.Link != catalog.PTG.HumanURL() {
			t.Fatalf("ptg source link = %q, want %q", source.Meta.Link, catalog.PTG.HumanURL())
		}
		if source.Flow.Version() != catalog.PTG.ExtractVersion {
			t.Fatalf("ptg source flow version = %d, want %d", source.Flow.Version(), catalog.PTG.ExtractVersion)
		}
	}
	if !found {
		t.Fatal("sources.All does not include the ptg source")
	}
}

func TestAllIncludesFilmSource(t *testing.T) {
	var found bool
	for _, source := range sources.All() {
		if source.ID != "film" {
			continue
		}
		found = true
		if source.URL != catalog.Film.PageURL() {
			t.Fatalf("film source URL = %q, want %q", source.URL, catalog.Film.PageURL())
		}
		if source.Meta.Title != "Arthouse & Classic Cinema" {
			t.Fatalf("film source title = %q", source.Meta.Title)
		}
		if source.Meta.Link != catalog.Film.HumanURL() {
			t.Fatalf("film source link = %q, want %q", source.Meta.Link, catalog.Film.HumanURL())
		}
		if source.Flow.Version() != catalog.Film.ExtractVersion {
			t.Fatalf("film source flow version = %d, want %d", source.Flow.Version(), catalog.Film.ExtractVersion)
		}
	}
	if !found {
		t.Fatal("sources.All does not include the film source")
	}
}

func TestAllIncludesMALStackSource(t *testing.T) {
	var found bool
	for _, source := range sources.All() {
		if source.ID != "mal-stack-82158" {
			continue
		}
		found = true
		if source.URL != malstack.PageURL(malstack.NewAnimeIPs) {
			t.Fatalf("mal-stack source URL = %q, want %q", source.URL, malstack.PageURL(malstack.NewAnimeIPs))
		}
		if source.Meta.Link != source.URL {
			t.Fatalf("mal-stack source link = %q, want %q", source.Meta.Link, source.URL)
		}
		if source.Meta.Title == "" || source.Meta.Description == "" {
			t.Fatal("mal-stack source metadata is incomplete")
		}
		if !source.Meta.ItemDescriptionsHTML {
			t.Fatal("mal-stack source must render its escaped note descriptions")
		}
		if source.Flow.Version() != malstack.ExtractVersion {
			t.Fatalf("mal-stack flow version = %d, want %d", source.Flow.Version(), malstack.ExtractVersion)
		}
		if _, ok := source.Flow.(rfs.ChangeFlow); !ok {
			t.Fatalf("mal-stack flow %T does not implement rfs.ChangeFlow", source.Flow)
		}
		if source.History != nil {
			t.Fatal("mal-stack change feed does not use catalog history")
		}
		if source.Interval != 0 {
			t.Fatalf("mal-stack source interval = %s, want the default", source.Interval)
		}
	}
	if !found {
		t.Fatal("sources.All does not include the mal-stack source")
	}
}

func TestAllIncludesMALStack38814Source(t *testing.T) {
	for _, source := range sources.All() {
		if source.ID != "mal-stack-38814" {
			continue
		}
		if source.URL != "https://myanimelist.net/stacks/38814" || source.Meta.Link != source.URL {
			t.Fatalf("unexpected stack URLs: fetch=%q link=%q", source.URL, source.Meta.Link)
		}
		if source.Meta.Title != "MyAnimeList: The Next Sakuga Shows" {
			t.Fatalf("unexpected stack title: %q", source.Meta.Title)
		}
		if source.Meta.Description == "" || !source.Meta.ItemDescriptionsHTML {
			t.Fatal("stack metadata must describe the feed and enable escaped HTML notes")
		}
		if _, ok := source.Flow.(rfs.ChangeFlow); !ok {
			t.Fatalf("stack flow %T does not implement rfs.ChangeFlow", source.Flow)
		}
		if source.Flow.Version() != 1 {
			t.Fatalf("stack flow version = %d, want 1", source.Flow.Version())
		}
		if source.History != nil || source.Interval != 0 {
			t.Fatal("stack must use a silent baseline, change history, and the default interval")
		}
		return
	}
	t.Fatal("sources.All does not include the mal-stack-38814 source")
}

// TestAllExcludesRemovedSources pins the registry after the tildes-comp,
// osmer-rain-trieste, ptv-remote-italy-jobs, and transport
// (arriva-udine, trieste-trasporti, apt-gorizia, trenitalia-disruptions) Flows
// were removed, and the one-piece, coached-essays, visionario, and
// citta-fiera Flows were added.
func TestAllExcludesRemovedSources(t *testing.T) {
	want := map[string]bool{
		"meltzer-5-star-matches": true,
		"ptg":                    true,
		"film":                   true,
		"seadex":                 true,
		"one-piece":              true,
		"acloserlisten":          true,
		"mal-stack-82158":        true,
		"mal-stack-38814":        true,
		"coached-essays":         true,
		"visionario":             true,
		"citta-fiera":            true,
	}
	all := sources.All()
	for _, source := range all {
		if !want[source.ID] {
			t.Fatalf("sources.All returned unexpected source %q", source.ID)
		}
		delete(want, source.ID)
	}
	for id := range want {
		t.Fatalf("sources.All missing %q", id)
	}
	if len(all) != 11 {
		t.Fatalf("len(sources.All()) = %d, want 11", len(all))
	}
}

func TestAllIncludesCittaFieraSource(t *testing.T) {
	var found bool
	for _, source := range sources.All() {
		if source.ID != "citta-fiera" {
			continue
		}
		found = true
		if source.URL != cittafiera.PageURL {
			t.Fatalf("citta-fiera source URL = %q, want %q", source.URL, cittafiera.PageURL)
		}
		if source.Meta.Link != cittafiera.HumanURL {
			t.Fatalf("citta-fiera source link = %q, want %q", source.Meta.Link, cittafiera.HumanURL)
		}
		if source.Meta.Title != "Cine Città Fiera" || source.Meta.Description == "" {
			t.Fatalf("citta-fiera source metadata = %#v", source.Meta)
		}
		if source.Meta.ItemDescriptionsHTML {
			t.Fatal("citta-fiera descriptions are plain text")
		}
		if source.Flow.Version() != cittafiera.ExtractVersion {
			t.Fatalf("citta-fiera source flow version = %d, want %d", source.Flow.Version(), cittafiera.ExtractVersion)
		}
		if _, ok := source.Flow.(rfs.AnnouncementFlow); !ok {
			t.Fatalf("citta-fiera flow %T does not implement rfs.AnnouncementFlow", source.Flow)
		}
		if _, ok := source.Flow.(rfs.DatedAnnouncementFlow); !ok {
			t.Fatalf("citta-fiera flow %T does not implement rfs.DatedAnnouncementFlow", source.Flow)
		}
		if _, ok := source.Flow.(rfs.AnnouncementIdentityFlow); !ok {
			t.Fatalf("citta-fiera flow %T does not implement rfs.AnnouncementIdentityFlow", source.Flow)
		}
		if source.Interval != 0 || source.History != nil {
			t.Fatal("citta-fiera source must use the default interval and an announcement checkpoint")
		}
	}
	if !found {
		t.Fatal("sources.All does not include the citta-fiera source")
	}
}

func TestAllIncludesVisionarioSource(t *testing.T) {
	var found bool
	for _, source := range sources.All() {
		if source.ID != "visionario" {
			continue
		}
		found = true
		if source.URL != visionario.PageURL {
			t.Fatalf("visionario source URL = %q, want %q", source.URL, visionario.PageURL)
		}
		if source.Meta.Link != visionario.HumanURL {
			t.Fatalf("visionario source link = %q, want %q", source.Meta.Link, visionario.HumanURL)
		}
		if source.Meta.Title != "Visionario and Cinema Centrale" || source.Meta.Description == "" {
			t.Fatalf("visionario source metadata = %#v", source.Meta)
		}
		if source.Meta.ItemDescriptionsHTML {
			t.Fatal("visionario descriptions are plain text")
		}
		if source.Flow.Version() != visionario.ExtractVersion {
			t.Fatalf("visionario source flow version = %d, want %d", source.Flow.Version(), visionario.ExtractVersion)
		}
		if _, ok := source.Flow.(rfs.AnnouncementFlow); !ok {
			t.Fatalf("visionario flow %T does not implement rfs.AnnouncementFlow", source.Flow)
		}
		if _, ok := source.Flow.(rfs.AnnouncementIdentityFlow); !ok {
			t.Fatalf("visionario flow %T does not implement rfs.AnnouncementIdentityFlow", source.Flow)
		}
		if source.Interval != 0 || source.History != nil {
			t.Fatal("visionario source must use the default interval and an announcement checkpoint")
		}
	}
	if !found {
		t.Fatal("sources.All does not include the visionario source")
	}
}

func TestAllIncludesOnePieceSource(t *testing.T) {
	var found bool
	for _, source := range sources.All() {
		if source.ID != "one-piece" {
			continue
		}
		found = true
		if source.URL != onepiece.PageURL {
			t.Fatalf("one-piece source URL = %q, want %q", source.URL, onepiece.PageURL)
		}
		if source.Meta.Link != onepiece.HumanURL {
			t.Fatalf("one-piece source link = %q, want %q", source.Meta.Link, onepiece.HumanURL)
		}
		if source.Meta.Title == "" || source.Meta.Description == "" {
			t.Fatal("one-piece source metadata is incomplete")
		}
		if source.Flow.Version() != onepiece.ExtractVersion {
			t.Fatalf("one-piece source flow version = %d, want %d", source.Flow.Version(), onepiece.ExtractVersion)
		}
	}
	if !found {
		t.Fatal("sources.All does not include the one-piece source")
	}
}

func TestAllIncludesCoachedSource(t *testing.T) {
	var found bool
	for _, source := range sources.All() {
		if source.ID != "coached-essays" {
			continue
		}
		found = true
		if source.URL != coached.PageURL {
			t.Fatalf("coached source URL = %q, want %q", source.URL, coached.PageURL)
		}
		if source.Meta.Link != coached.HumanURL {
			t.Fatalf("coached source link = %q, want %q", source.Meta.Link, coached.HumanURL)
		}
		if source.Meta.Title == "" || source.Meta.Description == "" {
			t.Fatal("coached source metadata is incomplete")
		}
		if source.Flow.Version() != coached.ExtractVersion {
			t.Fatalf("coached source flow version = %d, want %d", source.Flow.Version(), coached.ExtractVersion)
		}
		if source.Meta.ItemDescriptionsHTML {
			t.Fatal("coached source has no descriptions to render as HTML")
		}
		if source.Interval != 0 || source.History != nil {
			t.Fatal("coached source must use the default interval and a plain current-state projection")
		}
	}
	if !found {
		t.Fatal("sources.All does not include the coached source")
	}
}

// TestAllSourceRegistrationsAreUniqueAndWellFormed verifies every registration.
func TestAllSourceRegistrationsAreUniqueAndWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, source := range sources.All() {
		if source.ID == "" {
			t.Fatal("source with empty ID")
		}
		if seen[source.ID] {
			t.Fatalf("duplicate source ID %q", source.ID)
		}
		seen[source.ID] = true
		if !strings.HasPrefix(source.URL, "https://") {
			t.Fatalf("source %s URL %q is not HTTPS", source.ID, source.URL)
		}
		if !strings.HasPrefix(source.Meta.Link, "https://") {
			t.Fatalf("source %s link %q is not HTTPS", source.ID, source.Meta.Link)
		}
		if source.Meta.Title == "" || source.Meta.Description == "" {
			t.Fatalf("source %s metadata is incomplete", source.ID)
		}
		if source.Flow == nil {
			t.Fatalf("source %s has no Flow", source.ID)
		}
		if source.Flow.Version() <= 0 {
			t.Fatalf("source %s Flow version = %d, want a pinned positive version", source.ID, source.Flow.Version())
		}
	}
}

// TestAllSourcesAreValid checks every registered Source the way rfs does at
// startup, so a Flow whose capabilities do not pick one feed mode fails here
// rather than on its first poll in production.
func TestAllSourcesAreValid(t *testing.T) {
	if err := rfs.ValidateSources(sources.All()); err != nil {
		t.Fatal(err)
	}
}
