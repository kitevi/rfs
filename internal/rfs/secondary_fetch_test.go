package rfs

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
)

// secondaryRecorder captures the URLs a Poll routed through SecondaryFetch.
type secondaryRecorder struct{ urls []string }

func (r *secondaryRecorder) guard(_ context.Context, url string) error {
	r.urls = append(r.urls, url)
	return nil
}

// paginatedStub declares a numbered collection of one item per page.
type paginatedStub struct{}

func (paginatedStub) Version() int { return 1 }

func (paginatedStub) Pagination(page Page) (PageInfo, error) {
	var info PageInfo
	if err := json.Unmarshal(page, &info); err != nil {
		return PageInfo{}, err
	}
	return info, nil
}

func (paginatedStub) Extract(page Page) ([]ExtractedItem, error) {
	info, err := (paginatedStub{}).Pagination(page)
	if err != nil {
		return nil, err
	}
	return []ExtractedItem{{
		GUID:  "entity:" + strconv.Itoa(info.Page),
		Title: "Entity",
		Link:  "https://example.com/entity",
	}}, nil
}

func TestPollerSpacesFollowUpCollectionPages(t *testing.T) {
	store, err := OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	recorder := &secondaryRecorder{}
	fetcher := urlFetcher{pages: map[string]Page{
		"https://example.com/collection":        Page(`{"page":1,"totalPages":2,"totalItems":2}`),
		"https://example.com/collection?page=2": Page(`{"page":2,"totalPages":2,"totalItems":2}`),
	}}
	source := Source{ID: "collection", URL: "https://example.com/collection", Flow: paginatedStub{}}
	poller := Poller{Fetcher: fetcher, Store: store, SecondaryFetch: recorder.guard}

	if _, err := poller.Poll(t.Context(), source); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if len(recorder.urls) != 1 || recorder.urls[0] != "https://example.com/collection?page=2" {
		t.Fatalf("guarded follow-up requests = %v, want the page-2 fetch", recorder.urls)
	}
}

func TestPollerSpacesIdentityFetches(t *testing.T) {
	store, err := OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	recorder := &secondaryRecorder{}
	flow := &rewriteFlow{announcementTestFlow: announcementTestFlow{
		items:   []ExtractedItem{announcementItem("album:1"), announcementItem("track:9")},
		version: 1,
	}}
	source := Source{ID: "announcements", URL: "https://example.com/archive", Flow: flow}
	fetcher := urlFetcher{pages: map[string]Page{
		source.URL:                     Page("<page>"),
		"https://players.test/track:9": Page("album:1"),
	}}
	poller := Poller{Fetcher: fetcher, Store: store, SecondaryFetch: recorder.guard}

	if _, err := poller.Poll(t.Context(), source); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if len(recorder.urls) != 1 || recorder.urls[0] != "https://players.test/track:9" {
		t.Fatalf("guarded follow-up requests = %v, want the identity fetch", recorder.urls)
	}
}

func TestPollerSpacesAnnouncementMetadataFetches(t *testing.T) {
	server, _ := albumServer(t, &metadataPlan{})
	store, err := OpenInMemorySQLiteStore()
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	recorder := &secondaryRecorder{}
	flow := &resolverFlow{items: albums(2), base: server.URL}
	source := Source{ID: "albums", URL: server.URL, Flow: flow}
	poller := Poller{Fetcher: NewHTTPFetcher(server.Client()), Store: store, SecondaryFetch: recorder.guard}

	if _, err := poller.Poll(t.Context(), source); err != nil {
		t.Fatalf("poll: %v", err)
	}
	if len(recorder.urls) != 2 {
		t.Fatalf("guarded follow-up requests = %v, want one metadata fetch per album", recorder.urls)
	}
}
