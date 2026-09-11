package sources

import (
	"github.com/ppowo/rfs/internal/rfs"
	"github.com/ppowo/rfs/internal/sources/film"
	"github.com/ppowo/rfs/internal/sources/meltzer"
	"github.com/ppowo/rfs/internal/sources/ptg"
	"github.com/ppowo/rfs/internal/sources/seadex"
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
				Title:       "Private Trackers General",
				Description: "Latest /ptg/ opening posts via the board catalog.",
				Link:        ptg.HumanURL,
			},
			Flow:    ptg.Flow{},
			History: rfs.DefaultCatalogHistory(),
		},
		{
			ID:  "film",
			URL: film.PageURL,
			Meta: rfs.SourceMeta{
				Title:       "Arthouse & Classic Cinema",
				Description: "Latest /film/ opening posts via the board catalog.",
				Link:        film.HumanURL,
			},
			Flow:    film.Flow{},
			History: rfs.DefaultCatalogHistory(),
		},
	}
}
