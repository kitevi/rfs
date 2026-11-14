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

// NewHTTPHandler serves the index at / and each Source's feed at
// /feeds/<id>.xml and /feeds/<id>.html. The patterns answer GET and HEAD; the
// mux replies 405 with an Allow header to any other method.
func NewHTTPHandler(store SnapshotReader, sources []Source, build BuildInfo) http.Handler {
	byID := make(map[string]Source, len(sources))
	for _, source := range sources {
		byID[source.ID] = source
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		body, err := RenderHTMLIndex(sources, build)
		writeRendered(w, r, "text/html; charset=utf-8", body, err)
	})
	mux.HandleFunc("GET /feeds/{file}", func(w http.ResponseWriter, r *http.Request) {
		id, format, _ := strings.CutLast(r.PathValue("file"), ".")
		source, ok := byID[id]
		if !ok || (format != "xml" && format != "html") {
			http.NotFound(w, r)
			return
		}
		items, err := loadFeed(r.Context(), store, source)
		if err != nil {
			http.Error(w, "load feed snapshot", http.StatusInternalServerError)
			return
		}
		if format == "xml" {
			body, err := RenderRSS(source.Meta, items)
			writeRendered(w, r, "application/rss+xml; charset=utf-8", body, err)
			return
		}
		body, err := RenderHTMLFeed(id, source.Meta, items, build)
		writeRendered(w, r, "text/html; charset=utf-8", body, err)
	})
	return mux
}

// loadFeed reads a Source's Items the way its feed serves them. The store
// applies the default visible limit.
func loadFeed(ctx context.Context, store SnapshotReader, source Source) ([]Item, error) {
	if source.History == nil {
		return store.LoadSnapshot(ctx, source.ID)
	}
	return store.LoadVisibleHistory(ctx, source.ID, source.History.MinLiveReplies, source.History.VisibleLimit)
}

// writeRendered writes a rendered body with the given content type, or replies
// with a 500 if rendering failed. Rendering is deterministic, so a hash of the
// body is a strong ETag and a reader polling an unchanged feed gets a 304
// instead of the whole body.
func writeRendered(w http.ResponseWriter, r *http.Request, contentType string, body []byte, err error) {
	if err != nil {
		http.Error(w, "render page", http.StatusInternalServerError)
		return
	}
	sum := sha256.Sum256(body)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("ETag", `"`+hex.EncodeToString(sum[:16])+`"`)
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
}
