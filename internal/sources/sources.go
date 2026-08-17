package sources

import (
	"time"

	"github.com/ppowo/rfs/internal/rfs"
	"github.com/ppowo/rfs/internal/sources/film"
	"github.com/ppowo/rfs/internal/sources/meltzer"
	"github.com/ppowo/rfs/internal/sources/osmer"
	"github.com/ppowo/rfs/internal/sources/ptg"
	"github.com/ppowo/rfs/internal/sources/ptvjobs"
	"github.com/ppowo/rfs/internal/sources/tildes"
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
		{
			ID:  "tildes-comp",
			URL: tildes.PageURL,
			Meta: rfs.SourceMeta{
				Title:       "Tildes ~comp - top of the year",
				Description: "Most-upvoted ~comp topics on Tildes over the past year.",
				Link:        tildes.PageURL,
			},
			Flow: tildes.Flow{},
		},
		{
			ID:       "osmer-rain-trieste",
			URL:      osmer.PageURL,
			Interval: 30 * time.Minute,
			Meta: rfs.SourceMeta{
				Title:       "OSMER FVG - rain around Trieste",
				Description: "Coastal-zone OSMER forecasts (Z4, including Trieste) that explicitly mention rain, showers, precipitation, or thunderstorms.",
				Link:        osmer.PageURL,
			},
			Flow: osmer.Flow{},
		},
		{
			ID:  "ptv-remote-italy-jobs",
			URL: ptvjobs.PageURL,
			Meta: rfs.SourceMeta{
				Title:       "PTV Logistics - remote Italy jobs",
				Description: "Open PTV Logistics positions remote-eligible for Italy (Remote - Italy).",
				Link:        ptvjobs.PageURL,
			},
			Flow: ptvjobs.Flow{},
		},
	}
}
