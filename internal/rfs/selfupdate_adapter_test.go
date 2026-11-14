package rfs

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

// releaseServer serves a releases listing whose asset URLs point back at it,
// and records the Authorization header of every request by path.
func releaseServer(t *testing.T, listing func(base string) string) (githubReleases, map[string]string) {
	t.Helper()
	auth := map[string]string{}
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth[r.URL.Path] = r.Header.Get("Authorization")
		switch r.URL.Path {
		case "/releases":
			fmt.Fprint(w, listing(server.URL))
		case "/archive":
			fmt.Fprint(w, "archive")
		case "/insecure":
			http.Redirect(w, r, "http://example.test/rfs.tar.gz", http.StatusFound)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := server.Client()
	client.CheckRedirect = httpsRedirectsOnly
	return githubReleases{client: client, listURL: server.URL + "/releases", token: "listing-token"}, auth
}

func platformArchive(version string) string {
	return fmt.Sprintf("rfs_%s_%s_%s.tar.gz", version, runtime.GOOS, runtime.GOARCH)
}

func TestGitHubReleasesPicksHighestCompletePublishedVersion(t *testing.T) {
	client, auth := releaseServer(t, func(base string) string {
		asset := func(name, path string) string {
			return `{"name":"` + name + `","browser_download_url":"` + base + path + `"}`
		}
		return `[
			{"tag_name":"2026.709.200111","assets":[` + asset(platformArchive("2026.709.200111"), "/old") + "," + asset("checksums.txt", "/old-sums") + `]},
			{"tag_name":"2026.709.200112","assets":[` + asset(platformArchive("2026.709.200112"), "/archive") + "," + asset("rfs_2026.709.200112_plan9_mips.tar.gz", "/other") + "," + asset("checksums.txt", "/checksums") + `]},
			{"tag_name":"2026.709.200113","assets":[` + asset("checksums.txt", "/uploading") + `]},
			{"tag_name":"2027.101.1","draft":true,"assets":[` + asset(platformArchive("2027.101.1"), "/draft") + "," + asset("checksums.txt", "/draft-sums") + `]},
			{"tag_name":"2027.101.2","prerelease":true,"assets":[` + asset(platformArchive("2027.101.2"), "/pre") + "," + asset("checksums.txt", "/pre-sums") + `]},
			{"tag_name":"not-a-version"}
		]`
	})

	latest, found, err := client.Latest(t.Context())
	if err != nil || !found {
		t.Fatalf("Latest: found=%v err=%v", found, err)
	}
	if latest.Version != "2026.709.200112" || latest.ArchiveName != platformArchive("2026.709.200112") {
		t.Fatalf("latest = %+v, want the highest complete published version for this platform", latest)
	}
	if !strings.HasSuffix(latest.ArchiveURL, "/archive") || !strings.HasSuffix(latest.ChecksumsURL, "/checksums") {
		t.Fatalf("latest URLs = %q, %q", latest.ArchiveURL, latest.ChecksumsURL)
	}
	got, err := client.Download(t.Context(), latest.ArchiveURL)
	if err != nil || string(got) != "archive" {
		t.Fatalf("Download = %q, %v", got, err)
	}
	if auth["/releases"] != "Bearer listing-token" || auth["/archive"] != "" {
		t.Fatalf("Authorization by path = %v, want the token on the listing only", auth)
	}
}

func TestGitHubReleasesReportsNoInstallableRelease(t *testing.T) {
	client, _ := releaseServer(t, func(string) string { return "[]" })
	if _, found, err := client.Latest(t.Context()); err != nil || found {
		t.Fatalf("Latest: found=%v err=%v, want no release", found, err)
	}
}

func TestGitHubReleasesRefusesNonHTTPSAssetBeforeDownload(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests++ }))
	t.Cleanup(server.Close)
	client := githubReleases{client: server.Client()}
	if _, err := client.Download(t.Context(), server.URL+"/rfs.tar.gz"); err == nil {
		t.Fatal("non-HTTPS asset URL was accepted")
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0 for a non-HTTPS URL", requests)
	}
}

func TestGitHubReleasesRefusesRedirectOffHTTPS(t *testing.T) {
	client, _ := releaseServer(t, func(string) string { return "[]" })
	base := strings.TrimSuffix(client.listURL, "/releases")
	if _, err := client.Download(t.Context(), base+"/insecure"); err == nil {
		t.Fatal("a redirect to a non-HTTPS URL was followed")
	}
}

func TestReadUpdateAssetRejectsDataOverLimit(t *testing.T) {
	_, err := readUpdateAsset(strings.NewReader("four"), 3)
	if err == nil {
		t.Fatal("asset larger than its configured limit was accepted")
	}
}

// TestLiveSelfUpdateRelease is an opt-in probe of the real release feed. It runs
// the production workflow against the newest published release for this
// platform (find, download, verify, unpack) and hands the binary to a fake
// swapper, so nothing is installed:
//
//	RFS_TEST_SELFUPDATE_LIVE=1 go test ./internal/rfs -run TestLiveSelfUpdate -v -count=1
func TestLiveSelfUpdateRelease(t *testing.T) {
	if os.Getenv("RFS_TEST_SELFUPDATE_LIVE") == "" {
		t.Skip("set RFS_TEST_SELFUPDATE_LIVE=1 to probe the live release feed")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	swapper := &fakeSwapper{}
	result, err := Updater{Current: "dev", Client: newSelfUpdateClient(), Swapper: swapper}.Check(ctx)
	if err != nil {
		t.Fatalf("live self-update check: %v", err)
	}
	if result.Latest == "" {
		t.Skipf("no published release carries an archive for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if !result.Applied || len(swapper.got) == 0 {
		t.Fatalf("result = %+v, unpacked %d bytes; want the newest release verified and unpacked", result, len(swapper.got))
	}
	t.Logf("live release %s: verified and unpacked a %d byte binary", result.Latest, len(swapper.got))
}
