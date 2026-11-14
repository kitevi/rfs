package sources_test

import (
	"strings"
	"testing"

	"github.com/kitevi/rfs/internal/rfs"
	"github.com/kitevi/rfs/internal/sources"
	"github.com/kitevi/rfs/internal/sources/acloserlisten"
	"github.com/kitevi/rfs/internal/sources/catalog"
	"github.com/kitevi/rfs/internal/sources/cittafiera"
	"github.com/kitevi/rfs/internal/sources/coached"
	"github.com/kitevi/rfs/internal/sources/malstack"
	"github.com/kitevi/rfs/internal/sources/meltzer"
	"github.com/kitevi/rfs/internal/sources/onepiece"
	"github.com/kitevi/rfs/internal/sources/seadex"
	"github.com/kitevi/rfs/internal/sources/visionario"
)

// sourceCheck is what one registry entry must look like. Title and HTML
// descriptions are pinned only where the Source's own feed behaviour depends
// on them; everywhere else the common checks below cover them.
type sourceCheck struct {
	url      string
	link     string
	title    string
	version  int
	html     *bool
	change   bool
	announce bool
	dated    bool
	identity bool
	plain    bool // no catalog history and the default poll interval
}

func yes() *bool { value := true; return &value }
func no() *bool  { value := false; return &value }

// TestAll pins the whole registry: the exact set of eleven Sources, each one's
// URLs, metadata and capabilities, and that rfs.ValidateSources accepts it the
// way startup does.
func TestAll(t *testing.T) {
	want := map[string]sourceCheck{
		"citta-fiera": {
			url: cittafiera.PageURL, link: cittafiera.HumanURL,
			title: "Cine Città Fiera", version: cittafiera.ExtractVersion, html: no(),
			announce: true, dated: true, identity: true, plain: true,
		},
		"visionario": {
			url: visionario.PageURL, link: visionario.HumanURL,
			title: "Visionario and Cinema Centrale", version: visionario.ExtractVersion, html: no(),
			announce: true, identity: true, plain: true,
		},
		"acloserlisten": {
			url: acloserlisten.PageURL, link: acloserlisten.HumanURL,
			version: acloserlisten.ExtractVersion,
		},
		"coached-essays": {
			url: coached.PageURL, link: coached.HumanURL,
			version: coached.ExtractVersion, html: no(), plain: true,
		},
		"seadex": {
			url: seadex.PageURL, link: seadex.HumanURL,
			version: seadex.ExtractVersion,
		},
		"meltzer-5-star-matches": {
			url: meltzer.PageURL, link: meltzer.PageURL,
			version: meltzer.ExtractVersion,
		},
		"ptg": {
			url: catalog.PTG.PageURL(), link: catalog.PTG.HumanURL(),
			title: "Private Trackers General", version: catalog.PTG.ExtractVersion,
		},
		"film": {
			url: catalog.Film.PageURL(), link: catalog.Film.HumanURL(),
			title: "Arthouse & Classic Cinema", version: catalog.Film.ExtractVersion,
		},
		"one-piece": {
			url: onepiece.PageURL, link: onepiece.HumanURL,
			version: onepiece.ExtractVersion,
		},
		"mal-stack-82158": {
			url: malstack.PageURL(malstack.NewAnimeIPs), link: malstack.PageURL(malstack.NewAnimeIPs),
			version: malstack.ExtractVersion, html: yes(), change: true, plain: true,
		},
		"mal-stack-38814": {
			url: "https://myanimelist.net/stacks/38814", link: "https://myanimelist.net/stacks/38814",
			title: "MyAnimeList: The Next Sakuga Shows", version: 1, html: yes(), change: true, plain: true,
		},
	}

	all := sources.All()
	seen := make(map[string]bool, len(all))
	for _, source := range all {
		check, ok := want[source.ID]
		if !ok {
			t.Fatalf("sources.All returned unexpected source %q", source.ID)
		}
		if seen[source.ID] {
			t.Fatalf("duplicate source ID %q", source.ID)
		}
		seen[source.ID] = true
		checkSource(t, source, check)
	}
	if len(all) != len(want) {
		t.Fatalf("len(sources.All()) = %d, want %d", len(all), len(want))
	}
	for id := range want {
		if !seen[id] {
			t.Fatalf("sources.All missing %q", id)
		}
	}
	if err := rfs.ValidateSources(all); err != nil {
		t.Fatal(err)
	}
}

func checkSource(t *testing.T, source rfs.Source, check sourceCheck) {
	t.Helper()
	if source.URL != check.url {
		t.Errorf("%s URL = %q, want %q", source.ID, source.URL, check.url)
	}
	if source.Meta.Link != check.link {
		t.Errorf("%s link = %q, want %q", source.ID, source.Meta.Link, check.link)
	}
	if !strings.HasPrefix(source.URL, "https://") || !strings.HasPrefix(source.Meta.Link, "https://") {
		t.Errorf("%s URLs are not HTTPS: fetch=%q link=%q", source.ID, source.URL, source.Meta.Link)
	}
	if source.Meta.Title == "" || source.Meta.Description == "" {
		t.Errorf("%s metadata is incomplete: %#v", source.ID, source.Meta)
	}
	if check.title != "" && source.Meta.Title != check.title {
		t.Errorf("%s title = %q, want %q", source.ID, source.Meta.Title, check.title)
	}
	if source.Flow == nil {
		t.Fatalf("%s has no Flow", source.ID)
	}
	if check.version <= 0 {
		t.Errorf("%s expectation has no pinned positive version", source.ID)
	}
	if source.Flow.Version() != check.version {
		t.Errorf("%s Flow version = %d, want %d", source.ID, source.Flow.Version(), check.version)
	}
	if check.html != nil && source.Meta.ItemDescriptionsHTML != *check.html {
		t.Errorf("%s ItemDescriptionsHTML = %v, want %v", source.ID, source.Meta.ItemDescriptionsHTML, *check.html)
	}
	if check.change {
		if _, ok := source.Flow.(rfs.ChangeFlow); !ok {
			t.Errorf("%s flow %T does not implement rfs.ChangeFlow", source.ID, source.Flow)
		}
	}
	if check.announce {
		if _, ok := source.Flow.(rfs.AnnouncementFlow); !ok {
			t.Errorf("%s flow %T does not implement rfs.AnnouncementFlow", source.ID, source.Flow)
		}
	}
	if check.dated {
		if _, ok := source.Flow.(rfs.DatedAnnouncementFlow); !ok {
			t.Errorf("%s flow %T does not implement rfs.DatedAnnouncementFlow", source.ID, source.Flow)
		}
	}
	if check.identity {
		if _, ok := source.Flow.(rfs.AnnouncementIdentityFlow); !ok {
			t.Errorf("%s flow %T does not implement rfs.AnnouncementIdentityFlow", source.ID, source.Flow)
		}
	}
	if check.plain && (source.History != nil || source.Interval != 0) {
		t.Errorf("%s must use the default interval and no catalog history: interval=%s history=%v",
			source.ID, source.Interval, source.History != nil)
	}
}
