package sources

import (
	"github.com/ppowo/rfs/internal/rfs"
	"github.com/ppowo/rfs/internal/sources/film"
	"github.com/ppowo/rfs/internal/sources/meltzer"
	"github.com/ppowo/rfs/internal/sources/ptg"
)

func All() []rfs.Source {
	return []rfs.Source{
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
				Description: "Latest /ptg/ opening posts returned by Desuarchive search.",
				Link:        ptg.PageURL,
			},
			Flow: ptg.Flow{},
		},
		{
			ID:  "film",
			URL: film.PageURL,
			Meta: rfs.SourceMeta{
				Title:       "/film/ - Arthouse & Classic Cinema",
				Description: "Latest /film/ opening posts returned by 4plebs search.",
				Link:        film.PageURL,
			},
			Flow: film.Flow{},
		},
	}
}
