package sources_test

import (
	"testing"

	"github.com/ppowo/rfs/internal/sources"
	"github.com/ppowo/rfs/internal/sources/film"
	"github.com/ppowo/rfs/internal/sources/ptg"
	"github.com/ppowo/rfs/internal/sources/ptvjobs"
	"github.com/ppowo/rfs/internal/sources/tildes"
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
		if source.Meta.Link != ptg.PageURL {
			t.Fatalf("ptg source link = %q, want %q", source.Meta.Link, ptg.PageURL)
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
		if source.Meta.Link != film.PageURL {
			t.Fatalf("film source link = %q, want %q", source.Meta.Link, film.PageURL)
		}
		if source.Flow.Version() != film.ExtractVersion {
			t.Fatalf("film source flow version = %d, want %d", source.Flow.Version(), film.ExtractVersion)
		}
	}
	if !found {
		t.Fatal("sources.All does not include the film source")
	}
}

func TestAllIncludesTildesCompSource(t *testing.T) {
	var found bool
	for _, source := range sources.All() {
		if source.ID != "tildes-comp" {
			continue
		}
		found = true
		if source.URL != tildes.PageURL {
			t.Fatalf("tildes-comp source URL = %q, want %q", source.URL, tildes.PageURL)
		}
		if source.Meta.Title != "Tildes ~comp - top of the year" {
			t.Fatalf("tildes-comp source title = %q", source.Meta.Title)
		}
		if source.Meta.Link != tildes.PageURL {
			t.Fatalf("tildes-comp source link = %q, want %q", source.Meta.Link, tildes.PageURL)
		}
		if source.Flow.Version() != tildes.ExtractVersion {
			t.Fatalf("tildes-comp source flow version = %d, want %d", source.Flow.Version(), tildes.ExtractVersion)
		}
	}
	if !found {
		t.Fatal("sources.All does not include the tildes-comp source")
	}
}

func TestAllIncludesPTVRemoteItalyJobsSource(t *testing.T) {
	var found bool
	for _, source := range sources.All() {
		if source.ID != "ptv-remote-italy-jobs" {
			continue
		}
		found = true
		if source.URL != ptvjobs.PageURL {
			t.Fatalf("ptv-remote-italy-jobs source URL = %q, want %q", source.URL, ptvjobs.PageURL)
		}
		if source.Meta.Title != "PTV Logistics - remote Italy jobs" {
			t.Fatalf("ptv-remote-italy-jobs source title = %q", source.Meta.Title)
		}
		if source.Meta.Link != ptvjobs.PageURL {
			t.Fatalf("ptv-remote-italy-jobs source link = %q, want %q", source.Meta.Link, ptvjobs.PageURL)
		}
		if source.Flow.Version() != ptvjobs.ExtractVersion {
			t.Fatalf("ptv-remote-italy-jobs source flow version = %d, want %d", source.Flow.Version(), ptvjobs.ExtractVersion)
		}
	}
	if !found {
		t.Fatal("sources.All does not include the ptv-remote-italy-jobs source")
	}
}
