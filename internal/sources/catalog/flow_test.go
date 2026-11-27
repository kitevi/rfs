package catalog_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/kitevi/rfs/internal/rfs"
	"github.com/kitevi/rfs/internal/sources/catalog"
)

// The two catalog Flows extract the same thread shape from different boards,
// so one table holds both fixtures and every behaviour runs over each row.

type liveThread struct {
	page, guid, link, title string
	unix                    int64
}

type replyCounts struct {
	page string
	want []int
}

type matchingThreads struct {
	page string
	want []string
}

type invalidThreads struct {
	page string
	want string
}

type comFragment struct {
	page, description, title string
}

type rebuildCase struct{ in, want string }

type catalogCase struct {
	name    string
	flow    catalog.Flow
	version int

	live        liveThread
	replies     replyCounts
	matching    matchingThreads
	invalid     invalidThreads
	noMatch     string
	onlyInvalid string
	fragment    comFragment
	rebuild     []rebuildCase
}

var cases = []catalogCase{
	{
		name: "film", flow: catalog.Film, version: 7,
		live: liveThread{
			page: `[{"page":1,"threads":[{"no":222965645,"sub":"/film/","com":"Arthouse &amp; Classics<br><br>Guiltydition","time":1788463410}]}]`,
			guid: "film:222965645", link: "https://boards.4chan.org/tv/thread/222965645/",
			title: "Arthouse & Classics \u2014 Guiltydition", unix: 1788463410,
		},
		replies: replyCounts{
			page: `[{"page":1,"threads":[{"no":222965645,"sub":"/film/","com":"Edition","time":1788463410,"replies":120,"images":10}]}]`,
			want: []int{120},
		},
		matching: matchingThreads{
			page: `[{"page":1,"threads":[{"no":111,"sub":"/tv/ general","com":"other stuff","time":1788460000},{"no":222965645,"sub":"/film/","com":"first Edition","time":1788463410},{"no":222970000,"sub":"/FILM/ follow-up","com":"second Edition","time":1788500000},{"no":999,"com":"sticky without subject","time":1788460000}]}]`,
			want: []string{"film:222965645", "film:222970000"},
		},
		invalid: invalidThreads{
			page: `[{"page":1,"threads":[{"no":222965645,"sub":"/film/","com":"valid Edition","time":1788463410},{"no":0,"sub":"/film/ bad no","com":"x","time":1788463410},{"no":222965646,"sub":"/film/ no com","time":1788463410},{"no":222965647,"sub":"/film/ no time","com":"x"},{"no":222965648,"sub":"/film/ reply","com":"x","time":1788463410,"resto":222965645}]}]`,
			want: "film:222965645",
		},
		noMatch:     `[{"page":1,"threads":[{"no":111,"sub":"/tv/ general","com":"x","time":1788460000}]}]`,
		onlyInvalid: `[{"page":1,"threads":[{"no":222965645,"sub":"/film/","time":1788463410}]}]`,
		fragment: comFragment{
			page:        `[{"page":1,"threads":[{"no":221573858,"sub":"/film/","com":"Thread for the discussion of arthouse and classic cinema.<br><br>Don Carlo edition<br><span class=\"quote\">&gt;QOTD</span><br>What did you watch this week? <a href=\"https://example.com/chart\">chart</a>","time":1788463410}]}]`,
			description: "Thread for the discussion of arthouse and classic cinema.\n\n>QOTD\nWhat did you watch this week? chart",
			title:       "Arthouse & Classics \u2014 Don Carlo edition",
		},
		rebuild: []rebuildCase{
			{"/film/ - Arthouse & Classic Cinema \u2014 noir edition", "Arthouse & Classic Cinema \u2014 noir edition"},
			{"/film/ - Arthouse & Classic Cinema", "Arthouse & Classic Cinema"},
			{"Arthouse & Classic Cinema \u2014 noir edition", "Arthouse & Classic Cinema \u2014 noir edition"},
		},
	},
	{
		name: "ptg", flow: catalog.PTG, version: 8,
		live: liveThread{
			page: `[{"page":1,"threads":[{"no":109697201,"sub":"/ptg/ - Private Trackers General","com":"the tummies remain private Edition<br>FAQ: https://example.com","time":1788200342}]}]`,
			guid: "ptg:109697201", link: "https://boards.4chan.org/g/thread/109697201/",
			title: "Private Trackers General \u2014 the tummies remain private Edition", unix: 1788200342,
		},
		replies: replyCounts{
			page: `[{"page":1,"threads":[{"no":109697201,"sub":"/ptg/ - Private Trackers General","com":"Edition","time":1788200342,"replies":150,"images":20},{"no":109730000,"sub":"/ptg/ next","com":"Edition","time":1788300000}]}]`,
			want: []int{150, 0},
		},
		matching: matchingThreads{
			page: `[{"page":1,"threads":[{"no":1,"sub":"/lmg/ - Local Models General","com":"local stuff","time":1788200000},{"no":109697201,"sub":"/ptg/ - Private Trackers General","com":"first Edition","time":1788200342},{"no":109730000,"sub":"/PTG/ follow-up","com":"second Edition","time":1788300000},{"no":999,"com":"sticky without subject","time":1788200000}]}]`,
			want: []string{"ptg:109697201", "ptg:109730000"},
		},
		invalid: invalidThreads{
			page: `[{"page":1,"threads":[{"no":109697201,"sub":"/ptg/ - Private Trackers General","com":"valid Edition","time":1788200342},{"no":0,"sub":"/ptg/ bad no","com":"x","time":1788200342},{"no":109697202,"sub":"/ptg/ no com","time":1788200342},{"no":109697203,"sub":"/ptg/ no time","com":"x"},{"no":109697204,"sub":"/ptg/ reply","com":"x","time":1788200342,"resto":109697201}]}]`,
			want: "ptg:109697201",
		},
		noMatch:     `[{"page":1,"threads":[{"no":1,"sub":"/lmg/ - Local Models General","com":"x","time":1788200000}]}]`,
		onlyInvalid: `[{"page":1,"threads":[{"no":109697201,"sub":"/ptg/ - Private Trackers General","time":1788200342}]}]`,
		fragment: comFragment{
			page:        `[{"page":1,"threads":[{"no":109697201,"sub":"/ptg/ - Private Trackers General","com":"RED edition<br><br><span class=\"quote\">&gt;Not sure what private trackers are?</span><br>A private tracker is an invite-only site. FAQ<wbr>_link &amp; friends <a href=\"//boards.4chan.org/g/catalog#s=ptg\" class=\"quotelink\">&gt;&gt;&gt;/g/ptg</a>","time":1788200342}]}]`,
			description: ">Not sure what private trackers are?\nA private tracker is an invite-only site. FAQ_link & friends >>>/g/ptg",
			title:       "Private Trackers General \u2014 RED edition",
		},
		rebuild: []rebuildCase{
			{"/ptg/ - Private Trackers General \u2014 wired edition", "Private Trackers General \u2014 wired edition"},
			{"/ptg/ - Private Trackers General", "Private Trackers General"},
			{"Private Trackers General \u2014 wired edition", "Private Trackers General \u2014 wired edition"},
		},
	},
}

func TestCatalogFlows(t *testing.T) {
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("live thread", func(t *testing.T) {
				items, err := tc.flow.Extract(rfs.Page(tc.live.page))
				if err != nil {
					t.Fatalf("Extract returned error: %v", err)
				}
				if len(items) != 1 {
					t.Fatalf("expected 1 live thread, got %d: %#v", len(items), items)
				}
				item := items[0]
				if item.GUID != tc.live.guid || item.Link != tc.live.link || item.Title != tc.live.title {
					t.Fatalf("unexpected item: %#v", item)
				}
				wantDate := time.Unix(tc.live.unix, 0).UTC()
				if item.PubDate == nil || !item.PubDate.Equal(wantDate) {
					t.Fatalf("unexpected pubDate: %#v, want %v", item.PubDate, wantDate)
				}
				if item.Replies != 0 {
					t.Fatalf("unexpected replies without count: %d", item.Replies)
				}
			})
			t.Run("reply counts", func(t *testing.T) {
				items, err := tc.flow.Extract(rfs.Page(tc.replies.page))
				if err != nil {
					t.Fatalf("Extract returned error: %v", err)
				}
				if len(items) != len(tc.replies.want) {
					t.Fatalf("expected %d items, got %d", len(tc.replies.want), len(items))
				}
				for i, want := range tc.replies.want {
					if items[i].Replies != want {
						t.Fatalf("items[%d].Replies = %d, want %d", i, items[i].Replies, want)
					}
				}
			})
			t.Run("matching threads", func(t *testing.T) {
				items, err := tc.flow.Extract(rfs.Page(tc.matching.page))
				if err != nil {
					t.Fatalf("Extract returned error: %v", err)
				}
				if len(items) != len(tc.matching.want) {
					t.Fatalf("expected %d threads (no live-drop), got %d: %#v", len(tc.matching.want), len(items), items)
				}
				for i, guid := range tc.matching.want {
					if items[i].GUID != guid {
						t.Fatalf("items[%d].GUID = %q, want %q", i, items[i].GUID, guid)
					}
				}
			})
			t.Run("invalid threads", func(t *testing.T) {
				items, err := tc.flow.Extract(rfs.Page(tc.invalid.page))
				if err != nil {
					t.Fatalf("Extract returned error: %v", err)
				}
				if len(items) != 1 || items[0].GUID != tc.invalid.want {
					t.Fatalf("expected only the valid thread, got %#v", items)
				}
			})
			t.Run("no match", func(t *testing.T) {
				if _, err := tc.flow.Extract(rfs.Page(tc.noMatch)); err == nil {
					t.Fatal("expected an error when no thread matches the board")
				}
			})
			t.Run("only invalid matches", func(t *testing.T) {
				if _, err := tc.flow.Extract(rfs.Page(tc.onlyInvalid)); err == nil {
					t.Fatal("expected an error when matches exist but none are valid")
				}
			})
			t.Run("comment fragment", func(t *testing.T) {
				items, err := tc.flow.Extract(rfs.Page(tc.fragment.page))
				if err != nil {
					t.Fatalf("Extract returned error: %v", err)
				}
				if items[0].Description != tc.fragment.description {
					t.Fatalf("unexpected description: %q, want %q", items[0].Description, tc.fragment.description)
				}
				if items[0].Title != tc.fragment.title {
					t.Fatalf("unexpected title: %q, want %q", items[0].Title, tc.fragment.title)
				}
			})
			t.Run("invalid JSON", func(t *testing.T) {
				for _, body := range []string{`not json`, `{}`, `[]`} {
					if _, err := tc.flow.Extract(rfs.Page(body)); err == nil {
						t.Fatalf("expected an error for %q", body)
					}
				}
			})
			t.Run("legacy prefix", func(t *testing.T) {
				for _, c := range tc.rebuild {
					if got, err := tc.flow.RebuildStored(rfs.Item{Title: c.in}); err != nil || got.Title != c.want {
						t.Fatalf("RebuildStored(%q) = %q, want %q", c.in, got.Title, c.want)
					}
				}
			})
			t.Run("version pin", func(t *testing.T) {
				if tc.flow.Version() != tc.flow.ExtractVersion {
					t.Fatalf("Version() = %d, want extract version %d", tc.flow.Version(), tc.flow.ExtractVersion)
				}
				if tc.flow.ExtractVersion != tc.version {
					t.Fatalf("ExtractVersion = %d, want %d (history rebuild must replay over stored rows)", tc.flow.ExtractVersion, tc.version)
				}
			})
		})
	}
}

func TestCatalogTitlesNameTheEdition(t *testing.T) {
	const boilerplate = `<span class=\"quote\">&gt;Not sure what private trackers are all about?</span><br>A private tracker is an invite-only torrent website.`
	tests := []struct {
		flow      catalog.Flow
		sub, com  string
		wantTitle string
	}{
		{catalog.PTG, "/ptg/ - Private Trackers General", "Talia edition<br><br>" + boilerplate, "Private Trackers General — Talia edition"},
		{catalog.PTG, "/ptg/ - Private Trackers General", boilerplate, "Private Trackers General"},
		{catalog.PTG, "/ptg/ - Private Trackers General", "/ptg/ - Private Trackers General<br><br>" + boilerplate, "Private Trackers General"},
		{catalog.PTG, "/ptg/ - Private Trackers General - UTORRENT IS KILL EDITION", "UTORRENT IS KILL<br>BASED PTP GODS<br><br>" + boilerplate, "Private Trackers General - UTORRENT IS KILL EDITION"},
		{catalog.Film, "/film/", "Arthouse &amp; Classics<br><br>Deneuvedition<br><br><span class=\\\"quote\\\">&gt;QotD</span><br>Favourite Deneuve joint?", "Arthouse & Classics — Deneuvedition"},
		{catalog.Film, "/film/ - arthouse, classics and staging", "Arthouse &amp; Classics<br><br>Stagedition", "arthouse, classics and staging"},
	}
	for _, c := range tests {
		page := rfs.Page(`[{"page":1,"threads":[{"no":1,"sub":"` + c.sub + `","com":"` + c.com + `","time":1788200342}]}]`)
		items, err := c.flow.Extract(page)
		if err != nil {
			t.Fatalf("%q: %v", c.sub, err)
		}
		if items[0].Title != c.wantTitle {
			t.Errorf("sub %q com %q: title = %q, want %q", c.sub, c.com, items[0].Title, c.wantTitle)
		}
	}
}

// A ptg opening post repeats the general's links and rules in every thread,
// names the previous thread at the end, and repeats its own edition. The feed
// item carries none of them: what stays is the news and whatever else the
// poster wrote.
func TestPTGDescriptionsKeepOnlyTheThreadsOwnText(t *testing.T) {
	const (
		standingStart = `<span class="quote">&gt;Not sure what private trackers are all about?</span><br>A private tracker is an invite-only torrent website.`
		standingRules = `<br><span class="quote">&gt;READ THE OP</span><br><span class="quote">&gt;Don&#039;t ask for invites</span>`
		standingEnd   = `<br><span class="quote">&gt;If starting out you should interview for RED, rank to Torrent Master, wait on account age, and get invites from there. THAT&#039;S IT THAT IS ALL YOU HAVE TO DO</span>`
		previous      = `<br><br>Previous thread: <a href="/g/thread/110362431#p110362431" class="quotelink">&gt;&gt;110362431</a>`
	)
	const standing = standingStart + standingRules + standingEnd
	tests := []struct {
		name, com, want string
	}{
		{
			name: "news survives the boilerplate",
			com:  `No pedo edition<br><br>` + standing + `<br><br>NEWS<br><a href="/g/thread/110362431#p110369071" class="quotelink">&gt;&gt;110369071</a> oldtoons confirmed a honeypot` + previous,
			want: "NEWS\n>>110369071 oldtoons confirmed a honeypot",
		},
		{
			name: "a bare edition is the title alone",
			com:  `Pool edition<br><br>` + standing + previous,
			want: "",
		},
		{
			name: "a poster's own greentext after the block is kept",
			com:  `Pool edition<br><br>` + standing + `<br><br>&gt;inb4 lock`,
			want: ">inb4 lock",
		},
		{
			name: "an opener without the block keeps its greentext",
			com:  `Pool edition<br><br>&gt;Not sure what private trackers are?<br>A private tracker is an invite-only site.`,
			want: ">Not sure what private trackers are?\nA private tracker is an invite-only site.",
		},
		{
			name: "a half-rewritten block is not cut at all",
			com:  `Pool edition<br><br>` + standingStart + standingRules + `<br><br>and one more rule` + previous,
			want: ">Not sure what private trackers are all about?\nA private tracker is an invite-only torrent website.\n>READ THE OP\n>Don't ask for invites\n\nand one more rule",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			page := catalogJSON(t, map[string]any{
				"no": 110402817, "sub": "/ptg/ Private Trackers General", "com": tc.com, "time": 1788200342,
			})
			items, err := catalog.PTG.Extract(page)
			if err != nil {
				t.Fatalf("Extract returned error: %v", err)
			}
			if items[0].Description != tc.want {
				t.Fatalf("description = %q, want %q", items[0].Description, tc.want)
			}
		})
	}
}

// The general's news lines open with the post they discuss. The pointer is
// part of the news, so it stays; a lone pointer line says nothing and goes.
func TestPTGNewsKeepsItsText(t *testing.T) {
	page := catalogJSON(t, map[string]any{
		"no": 110402817, "sub": "/ptg/ Private Trackers General",
		"com":  "No pedo edition<br><br>NEWS<br><a href=\"/g/thread/110362431#p110369071\" class=\"quotelink\">&gt;&gt;110369071</a> oldtoons confirmed a honeypot<br><a href=\"/g/thread/110362431#p110372270\" class=\"quotelink\">&gt;&gt;110372270</a><br><br>Previous thread: <a href=\"/g/thread/110362431#p110362431\" class=\"quotelink\">&gt;&gt;110362431</a>",
		"time": 1788200342,
	})
	items, err := catalog.PTG.Extract(page)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if want := "NEWS\n>>110369071 oldtoons confirmed a honeypot"; items[0].Description != want {
		t.Fatalf("description = %q, want %q", items[0].Description, want)
	}
}

// Rebuilding a stored ptg row runs the same stripping, so a version bump
// rewrites accumulated descriptions instead of serving the old paste.
func TestPTGRebuildStripsTheStandingBlock(t *testing.T) {
	const standing = `<span class="quote">&gt;Not sure what private trackers are all about?</span><br>A private tracker is an invite-only torrent website.<br><span class="quote">&gt;READ THE OP</span><br><span class="quote">&gt;Don&#039;t ask for invites</span><br><span class="quote">&gt;If starting out you should interview for RED, rank to Torrent Master, wait on account age, and get invites from there. THAT&#039;S IT THAT IS ALL YOU HAVE TO DO</span>`
	metadata, err := json.Marshal(map[string]any{
		"no": 110402817, "sub": "/ptg/ Private Trackers General",
		"com": "Pool edition<br><br>" + standing, "time": 1788200342,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := catalog.PTG.RebuildStored(rfs.Item{GUID: "ptg:110402817", Title: "stale", Description: "stale", Metadata: string(metadata)})
	if err != nil {
		t.Fatalf("RebuildStored returned error: %v", err)
	}
	if got.Description != "" {
		t.Fatalf("description = %q, want the standing block gone", got.Description)
	}
	if want := "Private Trackers General \u2014 Pool edition"; got.Title != want {
		t.Fatalf("title = %q, want %q", got.Title, want)
	}
}

// catalogJSON wraps one catalog thread in the page shape the board API serves.
func catalogJSON(t *testing.T, thread map[string]any) rfs.Page {
	t.Helper()
	page, err := json.Marshal([]map[string]any{{"page": 1, "threads": []map[string]any{thread}}})
	if err != nil {
		t.Fatal(err)
	}
	return rfs.Page(page)
}
