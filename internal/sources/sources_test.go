package sources_test

import (
	"strings"
	"testing"

	"github.com/kitevi/rfs/internal/rfs"
	"github.com/kitevi/rfs/internal/sources"
	"github.com/kitevi/rfs/internal/sources/film"
	"github.com/kitevi/rfs/internal/sources/malstack"
	"github.com/kitevi/rfs/internal/sources/onepiece"
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

func TestAllIncludesMALStackSource(t *testing.T) {
	var found bool
	for _, source := range sources.All() {
		if source.ID != "mal-stack-82158" {
			continue
		}
		found = true
		if source.URL != malstack.PageURL {
			t.Fatalf("mal-stack source URL = %q, want %q", source.URL, malstack.PageURL)
		}
		if source.Meta.Link != malstack.HumanURL {
			t.Fatalf("mal-stack source link = %q, want %q", source.Meta.Link, malstack.HumanURL)
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
		if source.EmitInitial || source.EmitVersionChanges {
			t.Fatal("mal-stack change feed must start from a silent baseline")
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
		if source.EmitInitial || source.EmitVersionChanges || source.History != nil || source.Interval != 0 {
			t.Fatal("stack must use a silent baseline, change history, and the default interval")
		}
		return
	}
	t.Fatal("sources.All does not include the mal-stack-38814 source")
}

// TestAllExcludesRemovedSources pins the registry after the tildes-comp,
// osmer-rain-trieste, ptv-remote-italy-jobs, and transport
// (arriva-udine, trieste-trasporti, apt-gorizia, trenitalia-disruptions) Flows
// were removed, and the one-piece chapters Flow was added.
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
