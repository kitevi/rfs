package sources

import (
	"github.com/ppowo/rfs/internal/rfs"
	"github.com/ppowo/rfs/internal/sources/film"
	"github.com/ppowo/rfs/internal/sources/meltzer"
	"github.com/ppowo/rfs/internal/sources/ptg"
	"github.com/ppowo/rfs/internal/sources/seadex"
	"github.com/ppowo/rfs/internal/sources/tplfvg"
	"github.com/ppowo/rfs/internal/sources/trenitaliascioperi"
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
			ID:  "tpl-fvg-scioperi",
			URL: tplfvg.PageURL,
			Meta: rfs.SourceMeta{
				Title:       "TPL FVG bus strike notices",
				Description: "Operator-confirmed strike notices for Arriva Udine, Trieste Trasporti and APT Gorizia (including Monfalcone); published when covered services may be disrupted.",
				Link:        tplfvg.HumanURL,
			},
			Flow: tplfvg.Flow{},
			// A subscriber of a new feed should see the notices already in force
			// rather than an empty feed until the next upstream edit.
			EmitInitial: true,
		},
		{
			ID:  "trenitalia-scioperi",
			URL: trenitaliascioperi.PageURL,
			Meta: rfs.SourceMeta{
				Title:       "Trenitalia strike notices affecting FVG",
				Description: "Trenitalia passenger-service strike notices that affect travel in Friuli Venezia Giulia, including national notices whose scope covers the region.",
				Link:        trenitaliascioperi.HumanURL,
			},
			Flow:        trenitaliascioperi.Flow{},
			EmitInitial: true,
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
