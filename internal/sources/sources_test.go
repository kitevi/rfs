package sources_test

import (
	"strings"
	"testing"

	"github.com/ppowo/rfs/internal/rfs"
	"github.com/ppowo/rfs/internal/sources"
	"github.com/ppowo/rfs/internal/sources/film"
	"github.com/ppowo/rfs/internal/sources/ptg"
	"github.com/ppowo/rfs/internal/sources/tplfvg"
	"github.com/ppowo/rfs/internal/sources/trenitaliascioperi"
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
		"tpl-fvg-scioperi":       true,
		"trenitalia-scioperi":    true,
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
	if len(all) != 6 {
		t.Fatalf("len(sources.All()) = %d, want 6", len(all))
	}
}

// TestAllRegistersBusStrikeFeed pins the TPL FVG registration: official URLs,
// plain-text metadata and a change feed that fetches notice details.
func TestAllRegistersBusStrikeFeed(t *testing.T) {
	for _, source := range sources.All() {
		if source.ID != "tpl-fvg-scioperi" {
			continue
		}
		if source.URL != tplfvg.PageURL {
			t.Fatalf("source URL = %q, want %q", source.URL, tplfvg.PageURL)
		}
		if source.Meta.Link != tplfvg.HumanURL {
			t.Fatalf("source link = %q, want %q", source.Meta.Link, tplfvg.HumanURL)
		}
		if source.Meta.Title == "" || source.Meta.Description == "" {
			t.Fatalf("source metadata is incomplete: %#v", source.Meta)
		}
		if source.Meta.ItemDescriptionsHTML {
			t.Fatal("upstream notice text must not be rendered as HTML")
		}
		if source.History != nil {
			t.Fatal("strike feeds use ChangeFlow, not catalog history")
		}
		if !source.EmitInitial {
			t.Fatal("the notices already in force must be published on the first run")
		}
		if source.Flow.Version() != tplfvg.ExtractVersion {
			t.Fatalf("flow version = %d, want %d", source.Flow.Version(), tplfvg.ExtractVersion)
		}
		if _, ok := source.Flow.(rfs.ChangeFlow); !ok {
			t.Fatal("the bus strike Flow must compare complete observations")
		}
		if _, ok := source.Flow.(rfs.DetailFlow); !ok {
			t.Fatal("the bus strike Flow must declare the notice pages it needs")
		}
		return
	}
	t.Fatal("sources.All does not include the tpl-fvg-scioperi source")
}

// TestAllRegistersRailStrikeFeed pins the Trenitalia registration.
func TestAllRegistersRailStrikeFeed(t *testing.T) {
	for _, source := range sources.All() {
		if source.ID != "trenitalia-scioperi" {
			continue
		}
		if source.URL != trenitaliascioperi.PageURL {
			t.Fatalf("source URL = %q, want %q", source.URL, trenitaliascioperi.PageURL)
		}
		if source.Meta.Link != trenitaliascioperi.HumanURL {
			t.Fatalf("source link = %q, want %q", source.Meta.Link, trenitaliascioperi.HumanURL)
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
		if source.Flow.Version() != trenitaliascioperi.ExtractVersion {
			t.Fatalf("flow version = %d, want %d", source.Flow.Version(), trenitaliascioperi.ExtractVersion)
		}
		if _, ok := source.Flow.(rfs.ChangeFlow); !ok {
			t.Fatal("the rail strike Flow must compare complete observations")
		}
		return
	}
	t.Fatal("sources.All does not include the trenitalia-scioperi source")
}

// TestOnlyStrikeFeedsPublishTheirFirstObservation pins the initial-emission
// opt-in to the two operator strike feeds: SeaDex and every projection feed
// keep ADR 0008's silent baseline.
func TestOnlyStrikeFeedsPublishTheirFirstObservation(t *testing.T) {
	want := map[string]bool{"tpl-fvg-scioperi": true, "trenitalia-scioperi": true}
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
// not just the strike feeds.
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
