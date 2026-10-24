package rfs

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
}

func NewHTTPHandler(store SnapshotReader, sources []Source, build BuildInfo) http.Handler {
	byID := make(map[string]Source, len(sources))
	for _, source := range sources {
		byID[source.ID] = source
	}
	return HTTPHandler{store: store, sources: byID, orderedSources: sources, build: build}
}

func (h HTTPHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
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
	if presenter, ok := source.Flow.(ItemPresenter); ok {
		for i := range items {
			items[i] = presenter.PresentItem(items[i])
		}
	}

	switch format {
	case "xml":
		body, err := RenderRSS(source.Meta, items)
		writeRendered(w, r, "application/rss+xml; charset=utf-8", body, err)
	case "html":
		body, err := RenderHTMLFeed(sourceID, source.Meta, items, h.build)
		writeRendered(w, r, "text/html; charset=utf-8", body, err)
	default:
		http.NotFound(w, r)
	}
}

// writeRendered writes a rendered body with the given content type, or replies
// with a 500 if rendering failed. It deduplicates the render-and-write shape
// shared by the xml and html feed formats. Rendering is deterministic, so a hash
// of the body is a strong ETag and a reader polling an unchanged feed gets a
// 304 instead of the whole body.
func writeRendered(w http.ResponseWriter, r *http.Request, contentType string, body []byte, err error) {
	if err != nil {
		http.Error(w, "render feed", http.StatusInternalServerError)
		return
	}
	sum := sha256.Sum256(body)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:16])+`"`)
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
}

func (h HTTPHandler) serveIndex(w http.ResponseWriter, r *http.Request) {
	body, err := RenderHTMLIndex(h.orderedSources, h.build)
	if err != nil {
		http.Error(w, "render index", http.StatusInternalServerError)
		return
	}
	writeRendered(w, r, "text/html; charset=utf-8", body, nil)
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
