package malstack38814_test

import (
	"context"
	"encoding/xml"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kitevi/rfs/internal/rfs"
	"github.com/kitevi/rfs/internal/sources"
)

func TestPollKeepsMALStacksIndependent(t *testing.T) {
	bodies := map[string]string{
		"/mal-stack-38814": sparseStackHTML(),
		"/mal-stack-82158": strings.Replace(sparseStackHTML(), "stacks/38814", "stacks/82158", 1),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(bodies[r.URL.Path]))
	}))
	defer server.Close()
	store, err := rfs.OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var watched []rfs.Source
	byID := map[string]rfs.Source{}
	for _, source := range sources.All() {
		if source.ID != "mal-stack-38814" && source.ID != "mal-stack-82158" {
			continue
		}
		source.URL = server.URL + "/" + source.ID
		watched = append(watched, source)
		byID[source.ID] = source
	}
	if len(watched) != 2 {
		t.Fatalf("expected two registered MAL stacks, got %d", len(watched))
	}
	poller := rfs.Poller{
		Fetcher: rfs.NewHTTPFetcher(server.Client()),
		Store:   store,
		Clock:   &fixedClock{at: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)},
	}
	handler := rfs.NewHTTPHandler(store, watched, rfs.BuildInfo{})
	checkFeeds := func(want map[string]string) {
		t.Helper()
		for _, source := range watched {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest("GET", "/feeds/"+source.ID+".xml", nil))
			if w.Code != http.StatusOK {
				t.Fatalf("%s feed status %d: %s", source.ID, w.Code, w.Body.String())
			}
			var doc struct {
				Items []feedItem `xml:"channel>item"`
			}
			if err := xml.Unmarshal(w.Body.Bytes(), &doc); err != nil {
				t.Fatal(err)
			}
			if title := want[source.ID]; title == "" {
				if len(doc.Items) != 0 {
					t.Fatalf("%s feed changed unexpectedly: %+v", source.ID, doc.Items)
				}
			} else if len(doc.Items) != 1 || doc.Items[0].Title != title || doc.Items[0].GUID != source.ID+":1:63188" {
				t.Fatalf("%s feed = %+v, want %q at its own revision 1", source.ID, doc.Items, title)
			}
		}
	}
	for _, source := range watched {
		if _, err := poller.Poll(context.Background(), source); err != nil {
			t.Fatalf("%s baseline: %v", source.ID, err)
		}
	}
	checkFeeds(nil)

	bodies["/mal-stack-38814"] = strings.Replace(sparseStackHTML(), "<div class=\"intro\">\n      \n    </div>", `<div class="intro">New note.</div>`, 1)
	if _, err := poller.Poll(context.Background(), byID["mal-stack-38814"]); err != nil {
		t.Fatalf("38814 note edit: %v", err)
	}
	checkFeeds(map[string]string{"mal-stack-38814": "Notes updated: Ruri Dragon"})

	bodies["/mal-stack-82158"] = strings.Replace(stackHTML(0), "stacks/38814", "stacks/82158", 1)
	if _, err := poller.Poll(context.Background(), byID["mal-stack-82158"]); err != nil {
		t.Fatalf("82158 removal: %v", err)
	}
	checkFeeds(map[string]string{
		"mal-stack-38814": "Notes updated: Ruri Dragon",
		"mal-stack-82158": "Removed: Ruri Dragon",
	})
}
