package rfs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
)

// maxReleaseListBytes bounds the releases listing, which is a few kilobytes.
const maxReleaseListBytes int64 = 4 << 20

// githubReleases is the ReleaseClient backed by GitHub's public releases API.
// It only discovers and downloads: Updater enforces the no-downgrade rule,
// verifies checksums, extracts the expected binary, and performs the durable
// .bak swap.
type githubReleases struct {
	// client has no timeout of its own: every check runs under the monitor's
	// deadline, which also has to cover the archive download.
	client *http.Client
	// listURL returns the newest releases as JSON.
	listURL string
	// token, when set, authenticates the listing request only.
	token string
}

func newSelfUpdateClient() ReleaseClient {
	return githubReleases{
		client:  &http.Client{CheckRedirect: httpsRedirectsOnly},
		listURL: "https://api.github.com/repos/" + selfUpdateRepo + "/releases?per_page=10",
		token:   os.Getenv("GITHUB_TOKEN"),
	}
}

// httpsRedirectsOnly keeps a download on HTTPS across GitHub's redirect to its
// asset host.
func httpsRedirectsOnly(req *http.Request, via []*http.Request) error {
	if req.URL.Scheme != "https" {
		return fmt.Errorf("refuse non-HTTPS redirect to %q", req.URL)
	}
	if len(via) >= 10 {
		return errors.New("stopped after 10 redirects")
	}
	return nil
}

// Latest returns the highest published version that carries both the archive
// GoReleaser built for this platform and checksums.txt (see .goreleaser.yaml
// for the asset names). Drafts and pre-releases are ignored.
func (g githubReleases) Latest(ctx context.Context) (ReleaseCandidate, bool, error) {
	body, err := g.get(ctx, g.listURL, maxReleaseListBytes, g.token)
	if err != nil {
		return ReleaseCandidate{}, false, err
	}
	var releases []struct {
		TagName    string `json:"tag_name"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
		Assets     []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(body, &releases); err != nil {
		return ReleaseCandidate{}, false, fmt.Errorf("decode releases: %w", err)
	}
	var latest ReleaseCandidate
	found := false
	for _, release := range releases {
		version := strings.TrimPrefix(release.TagName, "v")
		if _, ok := parseVersion(version); !ok || release.Draft || release.Prerelease {
			continue
		}
		candidate := ReleaseCandidate{
			Version:     version,
			ArchiveName: fmt.Sprintf("rfs_%s_%s_%s.tar.gz", version, runtime.GOOS, runtime.GOARCH),
		}
		for _, asset := range release.Assets {
			switch asset.Name {
			case candidate.ArchiveName:
				candidate.ArchiveURL = asset.URL
			case "checksums.txt":
				candidate.ChecksumsURL = asset.URL
			}
		}
		// A release that is still uploading, or was built without this
		// platform, cannot be installed: an older complete release stays the
		// candidate and a later check picks the new one up.
		if candidate.ArchiveURL == "" || candidate.ChecksumsURL == "" {
			continue
		}
		if !found || isNewerVersion(latest.Version, candidate.Version) {
			latest, found = candidate, true
		}
	}
	return latest, found, nil
}

// Download reads one release asset over HTTPS, bounded by maxAssetBytes.
func (g githubReleases) Download(ctx context.Context, assetURL string) ([]byte, error) {
	return g.get(ctx, assetURL, maxAssetBytes, "")
}

// get reads one HTTPS resource of at most limit bytes. A non-empty token is
// sent as a bearer credential.
func (g githubReleases) get(ctx context.Context, rawURL string, limit int64, token string) ([]byte, error) {
	if !isHTTPSAssetURL(rawURL) {
		return nil, fmt.Errorf("refuse non-HTTPS URL %q", rawURL)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", rawURL, resp.Status)
	}
	data, err := readUpdateAsset(resp.Body, limit)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", rawURL, err)
	}
	return data, nil
}

func isHTTPSAssetURL(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}

func readUpdateAsset(reader io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("asset exceeds %d byte download limit", limit)
	}
	return data, nil
}

// validateReleaseChecksum checks an archive against its line in GoReleaser's
// checksums.txt, which lists "<sha256>  <filename>".
func validateReleaseChecksum(filename string, archive, checksums []byte) error {
	sum := sha256.Sum256(archive)
	for line := range strings.Lines(string(checksums)) {
		if fields := strings.Fields(line); len(fields) == 2 && fields[1] == filename {
			if strings.EqualFold(fields[0], hex.EncodeToString(sum[:])) {
				return nil
			}
			return fmt.Errorf("sha256 mismatch for %s", filename)
		}
	}
	return fmt.Errorf("checksums.txt has no entry for %s", filename)
}

// isNewerVersion compares release timestamps field by field (ADR 0004). A
// version that does not parse is never eligible, so bad build metadata cannot
// trigger a replacement or a downgrade. A dev build is older than any release.
func isNewerVersion(current, latest string) bool {
	next, ok := parseVersion(latest)
	if !ok {
		return false
	}
	if strings.TrimPrefix(current, "v") == "dev" {
		return true
	}
	running, ok := parseVersion(current)
	return ok && slices.Compare(running[:], next[:]) < 0
}

// parseVersion reads MAJOR.MINOR.PATCH, with or without a leading v.
func parseVersion(version string) ([3]int, bool) {
	var fields [3]int
	parts := strings.Split(strings.TrimPrefix(version, "v"), ".")
	if len(parts) != len(fields) {
		return fields, false
	}
	for i, part := range parts {
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return fields, false
		}
		fields[i] = number
	}
	return fields, true
}
