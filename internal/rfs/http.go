package rfs

import (
	"context"
	"net/http"
	"strings"
	"time"
)

type SnapshotReader interface {
	LoadSnapshot(context.Context, string) ([]Item, error)
	LoadVisibleHistory(context.Context, string, int, int) ([]Item, error)
}

type HTTPHandler struct {
	store          SnapshotReader
	sources        map[string]Source
	orderedSources []Source
	build          BuildInfo
	clock          Clock
}

func NewHTTPHandler(store SnapshotReader, sources []Source, build BuildInfo) http.Handler {
	return NewHTTPHandlerWithClock(store, sources, build, nil)
}

// NewHTTPHandlerWithClock is NewHTTPHandler with an explicit clock, so a caller
// can pin the instant liveness is judged at. A nil clock reads the wall clock.
func NewHTTPHandlerWithClock(store SnapshotReader, sources []Source, build BuildInfo, clock Clock) http.Handler {
	byID := make(map[string]Source, len(sources))
	for _, source := range sources {
		byID[source.ID] = source
	}
	return HTTPHandler{store: store, sources: byID, orderedSources: sources, build: build, clock: clock}
}

func (h HTTPHandler) now() time.Time {
	if h.clock == nil {
		return time.Now().UTC()
	}
	return h.clock.Now()
}

func (h HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.URL.Path == "/" {
		h.serveIndex(w, r)
		return
	}

	sourceID, format, ok := splitFeedPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	source, ok := h.sources[sourceID]
	if !ok {
		http.NotFound(w, r)
		return
	}

	at := h.now()
	var items []Item
	var err error
	if source.History != nil {
		minLive := source.History.MinLiveReplies
		if minLive < 0 {
			minLive = 0
		}
		limit := source.History.VisibleLimit
		if limit <= 0 {
			limit = 10
		}
		items, err = h.store.LoadVisibleHistory(r.Context(), sourceID, minLive, limit)
	} else {
		items, err = h.store.LoadSnapshot(r.Context(), sourceID)
	}
	if err != nil {
		http.Error(w, "load feed snapshot", http.StatusInternalServerError)
		return
	}
	items = liveItems(source, items, at)
	if presenter, ok := source.Flow.(ItemPresenter); ok {
		for i := range items {
			items[i] = presenter.PresentItem(items[i])
		}
	}

	switch format {
	case "xml":
		body, err := RenderRSS(source.Meta, items)
		writeRendered(w, "application/rss+xml; charset=utf-8", body, err)
	case "html":
		body, err := RenderHTMLFeed(sourceID, source.Meta, items, h.build)
		writeRendered(w, "text/html; charset=utf-8", body, err)
	default:
		http.NotFound(w, r)
	}
}

// writeRendered writes a rendered body with the given content type, or replies
// with a 500 if rendering failed. It deduplicates the render-and-write shape
// shared by the xml and html feed formats.
func writeRendered(w http.ResponseWriter, contentType string, body []byte, err error) {
	if err != nil {
		http.Error(w, "render feed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", contentType)
	_, _ = w.Write(body)
}

func (h HTTPHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
	body, err := RenderHTMLIndex(h.orderedSources, h.build)
	if err != nil {
		http.Error(w, "render index", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(body)
}

// splitFeedPath turns "/feeds/<id>.<ext>" into (id, ext, true). It rejects
// empty ids, ids containing slashes, and any path that is not under /feeds/.
func splitFeedPath(path string) (string, string, bool) {
	const prefix = "/feeds/"
	if !strings.HasPrefix(path, prefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(path, prefix)
	dot := strings.LastIndex(rest, ".")
	if dot <= 0 || dot == len(rest)-1 {
		return "", "", false
	}
	sourceID := rest[:dot]
	format := rest[dot+1:]
	if strings.Contains(sourceID, "/") {
		return "", "", false
	}
	return sourceID, format, true
}
