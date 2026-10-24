package sources

import (
	"github.com/kitevi/rfs/internal/rfs"
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

func All() []rfs.Source {
	return []rfs.Source{
		{
			ID:  "citta-fiera",
			URL: cittafiera.PageURL,
			Meta: rfs.SourceMeta{
				Title:       "Cine Città Fiera",
				Description: "New films at Cine Città Fiera in Udine, announced once the first screening is within two weeks.",
				Link:        cittafiera.HumanURL,
			},
			Flow: cittafiera.Flow{},
		},
		{
			ID:  "visionario",
			URL: visionario.PageURL,
			Meta: rfs.SourceMeta{
				Title:       "Visionario and Cinema Centrale",
				Description: "New films at Visionario and Cinema Centrale in Udine, announced once per film.",
				Link:        visionario.HumanURL,
			},
			Flow: visionario.Flow{},
		},
		{ID: "acloserlisten", URL: acloserlisten.PageURL, Meta: rfs.SourceMeta{
			Title: "A Closer Listen recommendations", Description: "New recommended releases, announced once per prose album name, Bandcamp album, standalone track, or YouTube video.", Link: acloserlisten.HumanURL,
		}, Flow: acloserlisten.Flow{}},
		{
			ID:  "coached-essays",
			URL: coached.PageURL,
			Meta: rfs.SourceMeta{
				Title:       "Coached essays",
				Description: "Essays by Rohan Mahtani on life and career strategy, announced once per essay.",
				Link:        coached.HumanURL,
			},
			Flow: coached.Flow{},
		},
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
			URL: catalog.PTG.PageURL(),
			Meta: rfs.SourceMeta{
				Title:       "Private Trackers General",
				Description: "Latest /ptg/ opening posts via the board catalog.",
				Link:        catalog.PTG.HumanURL(),
			},
			Flow:    catalog.PTG,
			History: rfs.DefaultCatalogHistory(),
		},
		{
			ID:  "film",
			URL: catalog.Film.PageURL(),
			Meta: rfs.SourceMeta{
				Title:       "Arthouse & Classic Cinema",
				Description: "Latest /film/ opening posts via the board catalog.",
				Link:        catalog.Film.HumanURL(),
			},
			Flow:    catalog.Film,
			History: rfs.DefaultCatalogHistory(),
		},
		{
			ID:  "one-piece",
			URL: onepiece.PageURL,
			Meta: rfs.SourceMeta{
				Title:       "One Piece chapters",
				Description: "New One Piece chapter releases from TCB Scans.",
				Link:        onepiece.HumanURL,
			},
			Flow: onepiece.Flow{},
		},
		{
			ID:  "mal-stack-82158",
			URL: malstack.PageURL(malstack.NewAnimeIPs),
			Meta: rfs.SourceMeta{
				Title:                "MyAnimeList: New Anime IPs with Exceptional Production Value",
				Description:          "Observed additions, removals, and curator note edits to the Interest Stack.",
				Link:                 malstack.PageURL(malstack.NewAnimeIPs),
				ItemDescriptionsHTML: true,
			},
			Flow: malstack.Flow{StackID: malstack.NewAnimeIPs},
		},
		{
			ID:  "mal-stack-38814",
			URL: malstack.PageURL(malstack.NextSakuga),
			Meta: rfs.SourceMeta{
				Title:                "MyAnimeList: The Next Sakuga Shows",
				Description:          "Observed additions, removals, and curator note edits to the Interest Stack.",
				Link:                 malstack.PageURL(malstack.NextSakuga),
				ItemDescriptionsHTML: true,
			},
			Flow: malstack.Flow{StackID: malstack.NextSakuga},
		},
	}
}
