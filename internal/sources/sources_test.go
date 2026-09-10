package sources_test

import (
	"strings"
	"testing"

	"github.com/ppowo/rfs/internal/rfs"
	"github.com/ppowo/rfs/internal/sources"
	"github.com/ppowo/rfs/internal/sources/film"
	"github.com/ppowo/rfs/internal/sources/ptg"
	"github.com/ppowo/rfs/internal/sources/trenitalia"
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
		if source.Meta.Title != "/ptg/ - Private Trackers General" {
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
		if source.Meta.Title != "/film/ - Arthouse & Classic Cinema" {
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
// osmer-rain-trieste, and ptv-remote-italy-jobs Flows were removed.
func TestAllExcludesRemovedSources(t *testing.T) {
	want := map[string]bool{
		"meltzer-5-star-matches": true,
		"ptg":                    true,
		"film":                   true,
		"seadex":                 true,
		"trenitalia-disruptions": true,
		"arriva-udine":           true,
		"trieste-trasporti":      true,
		"apt-gorizia":            true,
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
	if len(all) != 8 {
		t.Fatalf("len(sources.All()) = %d, want 8", len(all))
	}
}

func TestAllRegistersRailFeed(t *testing.T) {
	for _, source := range sources.All() {
		if source.ID != "trenitalia-disruptions" {
			continue
		}
		if source.URL != trenitalia.PageURL {
			t.Fatalf("source URL = %q, want %q", source.URL, trenitalia.PageURL)
		}
		if source.Meta.Link != trenitalia.HumanURL {
			t.Fatalf("source link = %q, want %q", source.Meta.Link, trenitalia.HumanURL)
		}
		if source.Meta.Title == "" || source.Meta.Description == "" {
			t.Fatalf("source metadata is incomplete: %#v", source.Meta)
		}
		if source.Meta.ItemDescriptionsHTML {
			t.Fatal("upstream notice text must not be rendered as HTML")
		}
		if source.EmitInitial != true {
			t.Fatal("the notices already in force must be published on the first run")
		}
		if source.Flow.Version() != trenitalia.ExtractVersion {
			t.Fatalf("flow version = %d, want %d", source.Flow.Version(), trenitalia.ExtractVersion)
		}
		if !source.EmitVersionChanges {
			t.Fatal("the widened rail scope must reach subscribers from before the widening")
		}
		if _, ok := source.Flow.(rfs.ChangeFlow); !ok {
			t.Fatal("the rail notice Flow must compare complete observations")
		}
		return
	}
	t.Fatal("sources.All does not include the trenitalia-disruptions source")
}

// TestOnlyRailFeedComparesAcrossExtractVersionChanges pins the version-change
// opt-in: a Source may compare across a bump only when its Flow treats an older
// stored payload as state rather than as a change, which today means the rail
// Flow.
func TestOnlyRailFeedComparesAcrossExtractVersionChanges(t *testing.T) {
	for _, source := range sources.All() {
		if source.EmitVersionChanges && source.ID != "trenitalia-disruptions" {
			t.Fatalf("source %s compares across a version change unexpectedly", source.ID)
		}
	}
}

// TestOnlyOperatorFeedsPublishTheirFirstObservation pins the initial-emission
// opt-in to the operator notice feeds: SeaDex and every projection feed keep
// ADR 0008's silent baseline.
func TestOnlyOperatorFeedsPublishTheirFirstObservation(t *testing.T) {
	want := map[string]bool{
		"trenitalia-disruptions": true,
		"arriva-udine":           true,
		"trieste-trasporti":      true,
		"apt-gorizia":            true,
	}
	for _, source := range sources.All() {
		if source.EmitInitial && !want[source.ID] {
			t.Fatalf("source %s publishes its first observation unexpectedly", source.ID)
		}
		if !source.EmitInitial && want[source.ID] {
			t.Fatalf("source %s must publish the notices already in force", source.ID)
		}
	}
}

// TestAllSourceRegistrationsAreUniqueAndWellFormed verifies every registration,
// not just the operator notice feeds.
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
