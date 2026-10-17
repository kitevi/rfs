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

// maxFetchBodyBytes bounds one fetched page. rfs holds whole pages in memory,
// so an oversized or never-ending response must fail the poll instead of
// growing without limit; a rejected fetch leaves the stored snapshot intact.
const maxFetchBodyBytes int64 = 16 << 20

// defaultFetchTimeout bounds one request made through the default client. Polls
// carry their own deadline, but a single stalled connection should not consume
// it, and every follow-up request inside a poll needs the same bound.
const defaultFetchTimeout = 30 * time.Second

var defaultHTTPClient = &http.Client{Timeout: defaultFetchTimeout}

type HTTPFetcher struct {
	client *http.Client
}

func NewHTTPFetcher(client *http.Client) HTTPFetcher {
	if client == nil {
		client = defaultHTTPClient
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
	req.Header.Set("User-Agent", "rfs/0.1 (+https://github.com/kitevi/rfs)")
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

	etag := resp.Header.Get("ETag")
	lastModified := resp.Header.Get("Last-Modified")
	result := FetchResult{Cache: cache}
	if etag != "" {
		result.Cache.ETag = etag
	}
	if lastModified != "" {
		result.Cache.LastModified = lastModified
	}

	switch resp.StatusCode {
	case http.StatusOK:
		// A 200 describes the body it just returned, so its validators replace
		// the stored ones outright. Keeping a validator the response omitted
		// would pin every later request to a representation the server no
		// longer serves: a matching If-None-Match then freezes the snapshot.
		result.Cache.ETag, result.Cache.LastModified = etag, lastModified
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBodyBytes+1))
		if err != nil {
			return FetchResult{}, err
		}
		if int64(len(body)) > maxFetchBodyBytes {
			return FetchResult{}, fmt.Errorf("fetch %s: body exceeds %d byte limit", req.URL, maxFetchBodyBytes)
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
