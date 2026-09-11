package seadex_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kitevi/rfs/internal/rfs"
	"github.com/kitevi/rfs/internal/sources/seadex"
)

// No JSONFetcher: cached metadata must go through the standard GET path.
type getOnly struct{ rfs.PageFetcher }

func TestCachedMetadataPublishesDiffWithoutAniListPOST(t *testing.T) {
	body := before
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method %s", r.Method)
			w.WriteHeader(403)
			return
		}
		if r.URL.Path == "/metadata" {
			calls++
			if r.URL.Query().Get("filter") != "alID=99698" {
				t.Errorf("bad filter: %s", r.URL.RawQuery)
			}
			w.Write([]byte(`{"items":[{"alID":99698,"title_english":"King's Game","coverImage_medium":"javascript:alert(1)"}]}`))
			return
		}
		w.Write([]byte(body))
	}))
	defer server.Close()
	store, err := rfs.OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	source := rfs.Source{ID: "seadex", URL: server.URL, Flow: seadex.Flow{CachedMetadataURL: server.URL + "/metadata"}}
	p := rfs.Poller{Fetcher: getOnly{rfs.NewHTTPFetcher(server.Client())}, Store: store}
	if _, err := p.Poll(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	if calls != 0 || len(feed(t, store, source)) != 0 {
		t.Fatal("baseline should be silent")
	}
	body = after
	if _, err := p.Poll(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	got := feed(t, store, source)
	if calls != 1 || len(got) != 1 || got[0].Title != "King's Game" || !strings.Contains(got[0].Description, "- NOGRP") || strings.Contains(got[0].Description, "<img") {
		t.Fatalf("bad feed: %+v", got)
	}
	if _, err := p.Poll(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(feed(t, store, source)) != 1 {
		t.Fatal("unchanged poll repeated notification")
	}
}
