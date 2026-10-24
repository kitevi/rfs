package rfs

import (
	"strings"
	"testing"
)

type rebuildingStubFlow struct{ projectionStubFlow }

func (rebuildingStubFlow) RebuildStored(item Item) (Item, error) { return item, nil }

func TestValidateSourceRejectsAmbiguousModes(t *testing.T) {
	announce := &announcementTestFlow{version: 1}
	cases := []struct {
		name   string
		source Source
		want   string
	}{
		{"missing ID", Source{URL: "https://example.com/", Flow: projectionStubFlow{}}, "without an ID"},
		{"missing flow", Source{ID: "s", URL: "https://example.com/"}, "missing flow"},
		{"relative URL", Source{ID: "s", URL: "/feed", Flow: projectionStubFlow{}}, "absolute HTTP(S) URL"},
		{"announce and compare", Source{ID: "s", URL: "https://example.com/", Flow: &compareAndAnnounceFlow{}}, "cannot both announce and compare"},
		{"history on announcements", Source{ID: "s", URL: "https://example.com/", Flow: announce, History: DefaultCatalogHistory()}, "catalog history only applies"},
		{"history on changes", Source{ID: "s", URL: "https://example.com/", Flow: &emissionFlow{}, History: DefaultCatalogHistory()}, "catalog history only applies"},
		{"rebuilder without history", Source{ID: "s", URL: "https://example.com/", Flow: rebuildingStubFlow{}}, "keeps none"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateSource(tc.source)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want one containing %q", err, tc.want)
			}
		})
	}
}

func TestValidateSourceAcceptsEachMode(t *testing.T) {
	for _, source := range []Source{
		{ID: "projection", URL: "https://example.com/", Flow: projectionStubFlow{}},
		{ID: "history", URL: "https://example.com/", Flow: rebuildingStubFlow{}, History: DefaultCatalogHistory()},
		{ID: "changes", URL: "https://example.com/", Flow: &emissionFlow{}},
		{ID: "announcements", URL: "https://example.com/", Flow: &announcementTestFlow{}},
	} {
		if err := ValidateSource(source); err != nil {
			t.Fatalf("%s: %v", source.ID, err)
		}
	}
}

func TestValidateSourcesRejectsDuplicateIDs(t *testing.T) {
	source := Source{ID: "s", URL: "https://example.com/", Flow: projectionStubFlow{}}
	err := ValidateSources([]Source{source, source})
	if err == nil || !strings.Contains(err.Error(), "duplicate source ID s") {
		t.Fatalf("error = %v, want a duplicate rejection", err)
	}
}
