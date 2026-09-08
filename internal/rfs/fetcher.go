package rfs

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

type FetchCache struct {
	ETag         string
	LastModified string

	// ExtractVersion is the derivation version that produced the stored
	// snapshot for this source, NOT an HTTP validator. The fetcher ignores it;
	// the poller sets it to the running Flow's version on every save so a 304
	// (which echoes no useful version) cannot clobber it back to 0.
	ExtractVersion int
}

type FetchStatus int

const (
	FetchModified FetchStatus = iota
	FetchNotModified
	FetchThrottled
)

type FetchResult struct {
	Status     FetchStatus
	Page       Page
	Cache      FetchCache
	RetryAfter time.Duration
}

type HTTPFetcher struct {
	client *http.Client
}

func NewHTTPFetcher(client *http.Client) HTTPFetcher {
	if client == nil {
		client = http.DefaultClient
	}
	return HTTPFetcher{client: client}
}

func (f HTTPFetcher) Fetch(ctx context.Context, url string, cache FetchCache) (FetchResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return FetchResult{}, err
	}
	return f.fetch(req, cache)
}

// FetchJSON posts a read-only JSON query through the same HTTP client and policy.
func (f HTTPFetcher) FetchJSON(ctx context.Context, url string, body []byte) (FetchResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return FetchResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	return f.fetch(req, FetchCache{})
}

func (f HTTPFetcher) fetch(req *http.Request, cache FetchCache) (FetchResult, error) {
	req.Header.Set("User-Agent", "rfs/0.1 (+https://github.com/ppowo/rfs)")
	if cache.ETag != "" {
		req.Header.Set("If-None-Match", cache.ETag)
	}
	if cache.LastModified != "" {
		req.Header.Set("If-Modified-Since", cache.LastModified)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return FetchResult{}, err
	}
	defer resp.Body.Close()

	result := FetchResult{Cache: cache}
	if etag := resp.Header.Get("ETag"); etag != "" {
		result.Cache.ETag = etag
	}
	if lastModified := resp.Header.Get("Last-Modified"); lastModified != "" {
		result.Cache.LastModified = lastModified
	}

	switch resp.StatusCode {
	case http.StatusOK:
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return FetchResult{}, err
		}
		result.Status = FetchModified
		result.Page = Page(body)
		return result, nil
	case http.StatusNotModified:
		result.Status = FetchNotModified
		return result, nil
	case http.StatusTooManyRequests, http.StatusServiceUnavailable:
		result.Status = FetchThrottled
		result.RetryAfter = parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())
		return result, nil
	default:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
		return FetchResult{}, fmt.Errorf("fetch %s: unexpected status %s", req.URL, resp.Status)
	}
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	if value == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(value); err == nil {
		if seconds < 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0
	}
	if when.Before(now) {
		return 0
	}
	return when.Sub(now)
}
