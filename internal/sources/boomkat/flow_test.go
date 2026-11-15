package boomkat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kitevi/rfs/internal/rfs"
	"github.com/kitevi/rfs/internal/rfstest"
)

// homepage.html is boomkat.com's homepage of 15 November 2026 cut to three of
// its page blocks, kept verbatim: Record Of The Week; Classic, which also
// names one release under a header flag; and a flagged collection of four.
func TestExtractReadsTheRecordOfTheWeek(t *testing.T) {
	items, err := Flow{}.Extract(rfstest.Fixture(t, "homepage.html"))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	want := rfs.ExtractedItem{
		GUID:  "boomkat:record-of-the-week:5upnjswv",
		Title: "Sara Udon — III",
		Link:  "https://boomkat.com/artists/sara-udon/releases/5upnjswv/iii",
		Description: "Download, Vinyl — A killer second suite of asymmetric dream architecture recorded in Osaka " +
			"by Conrad Standish ov CS + Kreme with Koshiro Hino and Yuki Nakagawa ov KAKUHAN, mining a sweetspot " +
			"between electroacoustic sorcery, drum machine ballistics and post rock poise, somewhere between Mark " +
			"Fell, Tortoise, Autechre and Bill Laswell in their taut, jazzy unpredictability and embrace of " +
			"negative space.",
	}
	if len(items) != 1 || items[0] != want {
		t.Fatalf("items = %#v, want only %#v", items, want)
	}
}

func TestExtractFindsTheBlockByItsFlagNotItsLayout(t *testing.T) {
	items, err := Flow{}.Extract(page(
		block("featured_release_classic", "Classic", blackhaine),
		block("a_layout_from_next_year", " record of&nbsp;the  week ", saraUdon),
	))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	want := rfs.ExtractedItem{
		GUID:        guidPrefix + "5upnjswv",
		Title:       "Sara Udon — III",
		Link:        "https://boomkat.com/artists/sara-udon/releases/5upnjswv/iii",
		Description: "Download, Vinyl — A killer second suite.",
	}
	if len(items) != 1 || items[0] != want {
		t.Fatalf("items = %#v, want only %#v", items, want)
	}
}

func TestExtractLinksTheReleasePageItself(t *testing.T) {
	body := strings.Replace(saraUdon, `href="/artists/sara-udon/releases/5upnjswv/iii"`,
		`href="https://boomkat.com/artists/sara-udon/releases/5upnjswv/iii?ref=home#review"`, 1)
	items, err := Flow{}.Extract(page(block("feature", flagLabel, body)))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(items) != 1 || items[0].Link != "https://boomkat.com/artists/sara-udon/releases/5upnjswv/iii" {
		t.Fatalf("items = %#v, want the bare release page", items)
	}
}

func TestExtractRejectsAPageWithoutTheFlag(t *testing.T) {
	_, err := Flow{}.Extract(page(
		block("release_collection", "Records of the Week", saraUdon),
		block("featured_release_classic", "Classic", blackhaine),
	))
	if err == nil || !strings.Contains(err.Error(), "no Record Of The Week") {
		t.Fatalf("error = %v, want a page without the pick rejected", err)
	}
}

func TestExtractRejectsAnUnreadableBlock(t *testing.T) {
	cases := map[string]struct {
		page rfs.Page
		want string
	}{
		"no release link": {page(block("feature", flagLabel,
			`<p><a href="/artists/sara-udon">Sara Udon</a></p><p>Text.</p>`)), "links no release"},
		"two releases": {page(block("feature", flagLabel, saraUdon+blackhaine)), "links releases 5upnjswv and mbmlpcxz"},
		"no artist link": {page(block("feature", flagLabel,
			strings.Replace(saraUdon, `<a href="/artists/sara-udon">Sara Udon</a>`, "Sara Udon", 1))), "no artist"},
		"only the Read More link": {page(block("feature", flagLabel,
			strings.Replace(saraUdon, `<a href="/artists/sara-udon/releases/5upnjswv/iii">III</a>`, "III", 1))), "no title"},
		"release on another host": {page(block("feature", flagLabel,
			strings.ReplaceAll(saraUdon, `href="/artists/sara-udon/releases`, `href="https://example.com/artists/sara-udon/releases`))), "links no release"},
		"flag outside a page block": {page(`<div class="page-block-header-flag"><p>Record Of The Week</p></div>` + saraUdon), "not inside a page block"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			items, err := Flow{}.Extract(test.page)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("items = %#v, error = %v, want an error naming %q", items, err, test.want)
			}
		})
	}
}

func TestEachPickIsAnnouncedOnce(t *testing.T) {
	flow := Flow{}
	steps := []struct {
		name      string
		body      string
		announced []string
		remember  []string
	}{
		{"first observation", saraUdon, []string{"5upnjswv"}, []string{"5upnjswv"}},
		{"same pick again", saraUdon, nil, []string{"5upnjswv"}},
		{"next week's pick", blackhaine, []string{"mbmlpcxz"}, []string{"5upnjswv", "mbmlpcxz"}},
		{"an earlier pick back", saraUdon, nil, []string{"5upnjswv", "mbmlpcxz"}},
	}
	var state json.RawMessage
	for _, step := range steps {
		decision, err := flow.Evaluate(state, observe(t, step.body))
		if err != nil {
			t.Fatalf("%s: evaluate: %v", step.name, err)
		}
		var got []string
		for _, item := range decision.Announcements {
			got = append(got, strings.TrimPrefix(item.GUID, guidPrefix))
		}
		if !slices.Equal(got, step.announced) {
			t.Fatalf("%s: announced %v, want %v", step.name, got, step.announced)
		}
		var saved checkpoint
		if err := json.Unmarshal(decision.Checkpoint, &saved); err != nil {
			t.Fatalf("%s: decode checkpoint: %v", step.name, err)
		}
		if saved.Version != CheckpointVersion || !slices.Equal(saved.Announced, step.remember) {
			t.Fatalf("%s: checkpoint = %#v, want %v", step.name, saved, step.remember)
		}
		state = decision.Checkpoint
	}
}

func TestEvaluateRejectsStateItCannotRead(t *testing.T) {
	current := observe(t, saraUdon)
	cases := map[string]struct {
		checkpoint string
		current    []rfs.ExtractedItem
		want       string
	}{
		"unsupported version":   {`{"version":99,"announced":["5upnjswv"]}`, current, "unsupported checkpoint version"},
		"empty checkpoint":      {`{"version":1,"announced":[]}`, current, "no releases"},
		"invalid release":       {`{"version":1,"announced":["../5upnjswv"]}`, current, "invalid checkpoint release"},
		"another source's item": {`{"version":1,"announced":["5upnjswv"]}`, []rfs.ExtractedItem{{GUID: "one-piece:chapter:1193"}}, "unexpected item GUID"},
		"empty observation":     {"", nil, "no Record Of The Week observed"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			var raw json.RawMessage
			if test.checkpoint != "" {
				raw = json.RawMessage(test.checkpoint)
			}
			decision, err := Flow{}.Evaluate(raw, test.current)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("decision = %#v, error = %v, want an error naming %q", decision, err, test.want)
			}
		})
	}
}

// TestPollPublishesEachRecordOfTheWeekOnce runs the Flow through rfs's own
// fetcher and store, starting with a challenged fetch.
func TestPollPublishesEachRecordOfTheWeekOnce(t *testing.T) {
	status, body := http.StatusForbidden, rfs.Page(`<html><head><title>Just a moment...</title></head></html>`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write(body)
	}))
	defer server.Close()
	store := rfstest.Store(t)
	source := rfs.Source{ID: "boomkat-record-of-the-week", URL: server.URL, Flow: Flow{}}
	poller := rfs.Poller{
		Fetcher: rfs.NewHTTPFetcher(server.Client()),
		Store:   store,
		Clock:   &hourly{at: time.Date(2026, 11, 15, 9, 0, 0, 0, time.UTC)},
	}
	ctx := context.Background()
	feed := func() []string {
		var guids []string
		for _, item := range rfstest.Feed(t, store, source) {
			guids = append(guids, strings.TrimPrefix(item.GUID, guidPrefix))
		}
		return guids
	}

	if _, err := poller.Poll(ctx, source); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("poll error = %v, want the challenge's 403 reported", err)
	}
	if got := feed(); len(got) != 0 {
		t.Fatalf("a challenged fetch published %v", got)
	}

	steps := []struct {
		name string
		page rfs.Page
		want []string
	}{
		{"homepage", rfstest.Fixture(t, "homepage.html"), []string{"5upnjswv"}},
		{"same homepage", rfstest.Fixture(t, "homepage.html"), []string{"5upnjswv"}},
		{"next week", page(block("feature", flagLabel, blackhaine)), []string{"mbmlpcxz", "5upnjswv"}},
		{"earlier pick back", page(block("feature", flagLabel, saraUdon)), []string{"mbmlpcxz", "5upnjswv"}},
	}
	status = http.StatusOK
	for _, step := range steps {
		body = step.page
		if _, err := poller.Poll(ctx, source); err != nil {
			t.Fatalf("%s: poll: %v", step.name, err)
		}
		if got := feed(); !slices.Equal(got, step.want) {
			t.Fatalf("%s: feed = %v, want %v", step.name, got, step.want)
		}
	}

	body = page(block("featured_release_classic", "Classic", saraUdon))
	if _, err := poller.Poll(ctx, source); err == nil || !strings.Contains(err.Error(), "no Record Of The Week") {
		t.Fatalf("poll error = %v, want a homepage without the flag rejected", err)
	}
	if got := feed(); !slices.Equal(got, []string{"mbmlpcxz", "5upnjswv"}) {
		t.Fatalf("a failed poll changed the feed to %v", got)
	}
}

// saraUdon and blackhaine are block bodies shaped like the homepage's: the
// formats line, the artist, the title, the review and the Read More button.
const (
	saraUdon = `<p>Download, Vinyl</p>` +
		`<p><a href="/artists/sara-udon">Sara Udon</a></p>` +
		`<p><a href="/artists/sara-udon/releases/5upnjswv/iii">III</a></p>` +
		`<p>A killer second suite.</p>` +
		`<p><a title="Read the full review of this product" href="/artists/sara-udon/releases/5upnjswv/iii">Read More</a></p>`
	blackhaine = `<p>Vinyl</p>` +
		`<p><a href="/artists/blackhaine">Blackhaine</a></p>` +
		`<p><a href="/artists/blackhaine/releases/mbmlpcxz/saints-in-the-wethouse">Saints in the Wethouse</a></p>` +
		`<p>Hits like a ton of bricks.</p>` +
		`<p><a href="/artists/blackhaine/releases/mbmlpcxz/saints-in-the-wethouse">Read More</a></p>`
)

// page wraps page blocks in a document.
func page(blocks ...string) rfs.Page {
	return rfs.Page("<html><body>" + strings.Join(blocks, "") + "</body></html>")
}

// block is a page block under a header flag, marked up like the homepage's.
func block(name, flag, body string) string {
	return `<section data-page-block-name="` + name + `"><header><div class="page-block-header-flag">` +
		`<div class="trix-content"><p>` + flag + `</p></div></div></header>` + body + `</section>`
}

// observe extracts a homepage whose Record Of The Week block has body.
func observe(t *testing.T, body string) []rfs.ExtractedItem {
	t.Helper()
	items, err := Flow{}.Extract(page(block("feature", flagLabel, body)))
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	return items
}

// hourly is a clock that moves an hour on each reading, so each poll's
// announcements sort above the last.
type hourly struct{ at time.Time }

func (c *hourly) Now() time.Time {
	c.at = c.at.Add(time.Hour)
	return c.at
}
