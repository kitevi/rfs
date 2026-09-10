package sources

import (
	"github.com/ppowo/rfs/internal/rfs"
	"github.com/ppowo/rfs/internal/sources/aptgorizia"
	"github.com/ppowo/rfs/internal/sources/arrivaudine"
	"github.com/ppowo/rfs/internal/sources/film"
	"github.com/ppowo/rfs/internal/sources/meltzer"
	"github.com/ppowo/rfs/internal/sources/notices"
	"github.com/ppowo/rfs/internal/sources/ptg"
	"github.com/ppowo/rfs/internal/sources/seadex"
	"github.com/ppowo/rfs/internal/sources/trenitalia"
	"github.com/ppowo/rfs/internal/sources/triestetrasporti"
)

func All() []rfs.Source {
	return []rfs.Source{
		{
			ID:  "seadex",
			URL: seadex.PageURL,
			Meta: rfs.SourceMeta{
				Title:                "SeaDex recommendation changes",
				Description:          "Observed changes to Best, Alt, Unmuxed Best and notes; silent initial baseline.",
				Link:                 seadex.HumanURL,
				ItemDescriptionsHTML: true,
			},
			Flow: seadex.Flow{CachedMetadataURL: seadex.CachedMetadataURL},
		},
		{
			ID:  "arriva-udine",
			URL: arrivaudine.PageURL,
			Meta: rfs.SourceMeta{
				Title:       "Arriva Udine service notices",
				Description: "Notices Arriva Udine publishes for the services it runs: strikes, timetable changes, route and stop changes and the works that alter them. The operator keeps old notices in the same archive, so the feed reads only the newest window.",
				Link:        arrivaudine.HumanURL,
			},
			Flow:        notices.Flow{Operator: "Arriva Udine", Parser: arrivaudine.ParseNotices},
			EmitInitial: true,
		},
		{
			ID:  "trieste-trasporti",
			URL: triestetrasporti.PageURL,
			Meta: rfs.SourceMeta{
				Title:       "Trieste Trasporti service notices",
				Description: "The notices Trieste Trasporti lists as in force: diversions, suspended stops and lines, weather and technical disruption, and timetable changes. Notices the operator has moved to its archive are never published.",
				Link:        triestetrasporti.HumanURL,
			},
			Flow:        notices.Flow{Operator: "Trieste Trasporti", Parser: triestetrasporti.ParseNotices},
			EmitInitial: true,
		},
		{
			ID:  "apt-gorizia",
			URL: aptgorizia.PageURL,
			Meta: rfs.SourceMeta{
				Title:       "APT Gorizia service notices",
				Description: "The service notices and route diversions APT Gorizia lists in its summary of the changes in force, covering the Gorizia network and the Monfalcone urban network it runs.",
				Link:        aptgorizia.HumanURL,
			},
			Flow:        notices.Flow{Operator: "APT Gorizia", Parser: aptgorizia.ParseNotices},
			EmitInitial: true,
		},
		{
			ID:  "trenitalia-disruptions",
			URL: trenitalia.PageURL,
			Meta: rfs.SourceMeta{
				Title:       "Trenitalia disruptions affecting FVG",
				Description: "Trenitalia notices that disrupt passenger services through Friuli Venezia Giulia: strikes, weather and technical incidents, delays, suspensions and planned works, including national notices whose scope covers the region.",
				Link:        trenitalia.HumanURL,
			},
			Flow: trenitalia.Flow{},
			// A subscriber of a new feed should see the notices already in force
			// rather than an empty feed until the next upstream edit.
			EmitInitial: true,
			// Subscribers from before the widening keep their history and still
			// receive the disruptions the wider scope now covers, so a version
			// change compares against the stored baseline instead of rebaselining
			// in silence. The Flow compares an older payload as state.
			EmitVersionChanges: true,
		},
		{
			ID:  "meltzer-5-star-matches",
			URL: meltzer.PageURL,
			Meta: rfs.SourceMeta{
				Title:       "Dave Meltzer 5-star wrestling matches",
				Description: "Professional wrestling matches rated 5 or more stars by Dave Meltzer.",
				Link:        meltzer.PageURL,
			},
			Flow: meltzer.Flow{},
		},
		{
			ID:  "ptg",
			URL: ptg.PageURL,
			Meta: rfs.SourceMeta{
				Title:       "/ptg/ - Private Trackers General",
				Description: "Latest /ptg/ opening posts via the 4chan catalog.",
				Link:        ptg.HumanURL,
			},
			Flow:    ptg.Flow{},
			History: rfs.DefaultCatalogHistory(),
		},
		{
			ID:  "film",
			URL: film.PageURL,
			Meta: rfs.SourceMeta{
				Title:       "/film/ - Arthouse & Classic Cinema",
				Description: "Latest /film/ opening posts via the 4chan catalog.",
				Link:        film.HumanURL,
			},
			Flow:    film.Flow{},
			History: rfs.DefaultCatalogHistory(),
		},
	}
}
