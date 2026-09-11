package sources_test

import (
	"strings"
	"testing"

	"github.com/kitevi/rfs/internal/sources"
	"github.com/kitevi/rfs/internal/sources/film"
	"github.com/kitevi/rfs/internal/sources/ptg"
)

func TestAllIncludesPTGSource(t *testing.T) {
	var found bool
	for _, source := range sources.All() {
		if source.ID != "ptg" {
			continue
		}
		found = true
		if source.URL != ptg.PageURL {
			t.Fatalf("ptg source URL = %q, want %q", source.URL, ptg.PageURL)
		}
		if source.Meta.Title != "Private Trackers General" {
			t.Fatalf("ptg source title = %q", source.Meta.Title)
		}
		if source.Meta.Link != ptg.HumanURL {
			t.Fatalf("ptg source link = %q, want %q", source.Meta.Link, ptg.HumanURL)
		}
		if source.Flow.Version() != ptg.ExtractVersion {
			t.Fatalf("ptg source flow version = %d, want %d", source.Flow.Version(), ptg.ExtractVersion)
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
		if source.URL != film.PageURL {
			t.Fatalf("film source URL = %q, want %q", source.URL, film.PageURL)
		}
		if source.Meta.Title != "Arthouse & Classic Cinema" {
			t.Fatalf("film source title = %q", source.Meta.Title)
		}
		if source.Meta.Link != film.HumanURL {
			t.Fatalf("film source link = %q, want %q", source.Meta.Link, film.HumanURL)
		}
		if source.Flow.Version() != film.ExtractVersion {
			t.Fatalf("film source flow version = %d, want %d", source.Flow.Version(), film.ExtractVersion)
		}
	}
	if !found {
		t.Fatal("sources.All does not include the film source")
	}
}

// TestAllExcludesRemovedSources pins the registry after the tildes-comp,
// osmer-rain-trieste, ptv-remote-italy-jobs, and transport
// (arriva-udine, trieste-trasporti, apt-gorizia, trenitalia-disruptions) Flows
// were removed.
func TestAllExcludesRemovedSources(t *testing.T) {
	want := map[string]bool{
		"meltzer-5-star-matches": true,
		"ptg":                    true,
		"film":                   true,
		"seadex":                 true,
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
	if len(all) != 4 {
		t.Fatalf("len(sources.All()) = %d, want 4", len(all))
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
