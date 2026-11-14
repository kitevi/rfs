package rfs

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestIsNewerVersion is the self-update gate: it decides whether the latest
// GitHub release is newer than the running build's baked-in version. It must
// normalize a leading "v" (goreleaser bakes the tag without "v"; the GitHub
// tag_name carries it), compare X.Y.Z numerically, and rank a "dev" local
// build below any real release. cmd/rfs skips constructing an updater for dev
// builds, but the comparator itself remains explicit about the ordering.
func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		name            string
		current, latest string
		want            bool
	}{
		{"patch newer", "0.1.0", "0.1.1", true},
		{"minor newer", "0.1.0", "0.2.0", true},
		{"major newer", "1.0.0", "2.0.0", true},
		{"equal is not newer", "0.1.0", "0.1.0", false},
		{"older is not newer", "0.2.0", "0.1.0", false},
		{"normalize leading v on latest", "0.1.0", "v0.1.2", true},
		{"normalize leading v on current", "v0.1.0", "0.1.2", true},
		{"dev is always older", "dev", "0.0.1", true},
		{"dev vs dev is not newer", "dev", "dev", false},
		// Time-based versions are semver-shaped timestamps (YYYY.MD.HMS per the
		// release workflow, where MD = month·100+day and HMS = HH·10000+MM·100+SS):
		// a later timestamp is newer, compared field-by-field as numbers.
		{"time-based newer second", "2026.709.200111", "2026.709.200112", true},
		{"time-based newer minute", "2026.709.200111", "2026.709.201234", true},
		{"time-based newer day", "2026.708.120000", "2026.709.214200", true},
		{"time-based newer month", "2026.731.235959", "2026.801.000000", true},
		{"time-based newer year", "2026.1231.235959", "2027.101.000000", true},
		{"time-based older is not newer", "2026.709.214300", "2026.709.200111", false},
		{"time-based equal is not newer", "2026.709.200111", "2026.709.200111", false},
		{"dev older than time-based", "dev", "2026.709.200111", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNewerVersion(tt.current, tt.latest); got != tt.want {
				t.Fatalf("isNewerVersion(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
			}
		})
	}
}

// TestValidateReleaseChecksum verifies the maintained adapter's checksums.txt
// validator against the checksum format GoReleaser publishes. The expected
// hash is the well-known sha256 of "hello", rather than a value recomputed by
// this test.
func TestValidateReleaseChecksum(t *testing.T) {
	// sha256("hello") = 2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824
	archive := []byte("hello")
	name := "rfs_0.1.0_linux_amd64.tar.gz"
	checksums := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824  " + name + "\n"
	if err := validateReleaseChecksum(name, archive, []byte(checksums)); err != nil {
		t.Fatalf("valid checksum rejected: %v", err)
	}

	if err := validateReleaseChecksum(name, []byte("world"), []byte(checksums)); err == nil {
		t.Fatal("tampered archive accepted (checksum mismatch not detected)")
	}
	if err := validateReleaseChecksum("rfs_0.1.0_linux_arm64.tar.gz", archive, []byte(checksums)); err == nil {
		t.Fatal("accepted a checksum line for a different filename")
	}
}

// TestExtractBinaryFromArchive extracts the rfs binary from a verified tar.gz
// archive (goreleaser packs a single binary named "rfs"). The expected bytes
// are a fixed literal the test writes into the archive, not a value derived
// the way extraction computes it.
func TestExtractBinaryFromArchive(t *testing.T) {
	archive := makeArchive(t, map[string]string{"rfs": "BINARY-CONTENT"})
	got, err := extractBinaryFromArchive(archive)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if string(got) != "BINARY-CONTENT" {
		t.Fatalf("extracted %q, want %q", got, "BINARY-CONTENT")
	}

	// An archive without an rfs entry is an error.
	noRfs := makeArchive(t, map[string]string{"README": "hi"})
	if _, err := extractBinaryFromArchive(noRfs); err == nil {
		t.Fatal("extracted a binary from an archive that has no rfs entry")
	}
}

func TestExtractBinaryFromArchiveRejectsNonRegularRFS(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	if err := tw.WriteHeader(&tar.Header{Name: "rfs", Typeflag: tar.TypeSymlink, Linkname: "/tmp/other-rfs"}); err != nil {
		t.Fatalf("write symlink header: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}

	if _, err := extractBinaryFromArchive(buf.Bytes()); err == nil {
		t.Fatal("accepted a non-regular rfs archive entry")
	}
}

func TestExtractBinaryFromArchiveRejectsOversizedRFS(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	if err := tw.WriteHeader(&tar.Header{Name: "rfs", Mode: 0o755, Size: maxExtractedBinaryBytes + 1}); err != nil {
		t.Fatalf("write oversized header: %v", err)
	}
	// The extractor rejects the declared size before reading file content, so a
	// deliberately incomplete tar stream is sufficient and avoids allocating it.
	_ = tw.Close()
	if err := gw.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}

	if _, err := extractBinaryFromArchive(buf.Bytes()); err == nil {
		t.Fatal("accepted an oversized rfs archive entry")
	}
}

func makeArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(content))}); err != nil {
			t.Fatalf("write header: %v", err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("write file: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

type fakeReleaseClient struct {
	latest ReleaseCandidate
	found  bool
	err    error
	assets map[string][]byte
	calls  []string
}

func (f *fakeReleaseClient) Latest(context.Context) (ReleaseCandidate, bool, error) {
	return f.latest, f.found, f.err
}

func (f *fakeReleaseClient) Download(_ context.Context, url string) ([]byte, error) {
	f.calls = append(f.calls, url)
	if f.err != nil {
		return nil, f.err
	}
	asset, ok := f.assets[url]
	if !ok {
		return nil, fmt.Errorf("fake: no asset for %s", url)
	}
	return asset, nil
}

type fakeSwapper struct {
	got []byte
}

func (f *fakeSwapper) Swap(b []byte) error { f.got = b; return nil }

type fakeUpdateChecker struct {
	result UpdateCheckResult
	err    error
	calls  int
}

func (f *fakeUpdateChecker) Check(context.Context) (UpdateCheckResult, error) {
	f.calls++
	return f.result, f.err
}

func TestUpdateMonitorDefaultsToTenMinuteChecks(t *testing.T) {
	monitor := newUpdateMonitor(&fakeUpdateChecker{}, UpdateMonitorConfig{})
	if got := monitor.checkInterval(); got != 10*time.Minute {
		t.Fatalf("default update check interval = %s, want 10m", got)
	}
}

func TestUpdateMonitorReportsAppliedUpdateOnce(t *testing.T) {
	checker := &fakeUpdateChecker{result: UpdateCheckResult{Current: "0.1.0", Latest: "0.2.0", Applied: true}}
	monitor := newUpdateMonitor(checker, UpdateMonitorConfig{CheckInterval: time.Hour, CheckTimeout: time.Second})
	ctx := t.Context()

	events := monitor.Run(ctx)
	select {
	case event := <-events:
		if event.Err != nil {
			t.Fatalf("update event error: %v", event.Err)
		}
		if !event.Result.Applied || event.Result.Current != "0.1.0" || event.Result.Latest != "0.2.0" {
			t.Fatalf("update event = %+v, want applied 0.1.0 -> 0.2.0", event.Result)
		}
	case <-time.After(time.Second):
		t.Fatal("UpdateMonitor did not report its initial update check")
	}

	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("UpdateMonitor continued after applying an update")
		}
	case <-time.After(time.Second):
		t.Fatal("UpdateMonitor did not stop after applying an update")
	}
	if checker.calls != 1 {
		t.Fatalf("checker calls = %d, want 1", checker.calls)
	}
}

func TestUpdateMonitorTimesOutCheckWithoutCancellingItsLifetime(t *testing.T) {
	checker := updateCheckerFunc(func(ctx context.Context) (UpdateCheckResult, error) {
		<-ctx.Done()
		return UpdateCheckResult{}, ctx.Err()
	})
	monitor := newUpdateMonitor(checker, UpdateMonitorConfig{CheckInterval: time.Hour, CheckTimeout: 20 * time.Millisecond})
	ctx := t.Context()

	select {
	case event := <-monitor.Run(ctx):
		if !errors.Is(event.Err, context.DeadlineExceeded) {
			t.Fatalf("update event error = %v, want check deadline exceeded", event.Err)
		}
		if ctx.Err() != nil {
			t.Fatal("update check timeout cancelled the monitor lifetime")
		}
	case <-time.After(time.Second):
		t.Fatal("UpdateMonitor did not enforce its check timeout")
	}
}

type updateCheckerFunc func(context.Context) (UpdateCheckResult, error)

func (f updateCheckerFunc) Check(ctx context.Context) (UpdateCheckResult, error) { return f(ctx) }

// TestUpdaterCheckAppliesNewerRelease drives the full self-update pipeline with
// a release client seam: a newer release is fetched, the matching archive and
// checksums are downloaded, the archive is verified, the rfs binary is
// extracted, and the swapper receives it. Equal versions apply nothing. A
// tampered archive is refused before the swapper is ever touched.
func TestUpdaterCheckAppliesNewerRelease(t *testing.T) {
	binary := []byte("NEW-BINARY")
	archive := makeArchive(t, map[string]string{"rfs": string(binary)})
	sum := sha256.Sum256(archive)
	archiveName := "rfs_0.2.0_linux_amd64.tar.gz"
	checksums := hex.EncodeToString(sum[:]) + "  " + archiveName + "\n"

	candidate := ReleaseCandidate{
		Version:      "0.2.0",
		ArchiveName:  archiveName,
		ArchiveURL:   "archive",
		ChecksumsURL: "checksums",
	}
	client := &fakeReleaseClient{
		latest: candidate,
		found:  true,
		assets: map[string][]byte{"archive": archive, "checksums": []byte(checksums)},
	}
	swapper := &fakeSwapper{}
	u := Updater{Current: "0.1.0", Client: client, Swapper: swapper}

	result, err := u.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !result.Applied {
		t.Fatal("expected an update to be applied")
	}
	if result.Current != "0.1.0" || result.Latest != "0.2.0" {
		t.Fatalf("result = %+v, want current 0.1.0 latest 0.2.0", result)
	}
	if !bytes.Equal(swapper.got, binary) {
		t.Fatalf("swapper got %q, want the extracted binary %q", swapper.got, binary)
	}
	if len(client.calls) != 2 || client.calls[0] != "checksums" || client.calls[1] != "archive" {
		t.Fatalf("download calls = %v, want checksums then archive", client.calls)
	}

	// Equal version: nothing is downloaded or swapped.
	equalClient := &fakeReleaseClient{latest: candidate, found: true, assets: client.assets}
	equalSwapper := &fakeSwapper{}
	equalUpdater := Updater{Current: "0.2.0", Client: equalClient, Swapper: equalSwapper}
	result2, err := equalUpdater.Check(context.Background())
	if err != nil || result2.Applied {
		t.Fatalf("equal version: result=%+v err=%v", result2, err)
	}
	if result2.Current != "0.2.0" || result2.Latest != "0.2.0" {
		t.Fatalf("equal version result = %+v, want current/latest 0.2.0", result2)
	}
	if len(equalClient.calls) != 0 || equalSwapper.got != nil {
		t.Fatal("equal version downloaded or swapped")
	}

	// No release yet: the check reports the current version and applies nothing.
	noReleaseClient := &fakeReleaseClient{}
	noReleaseUpdater := Updater{Current: "0.1.0", Client: noReleaseClient, Swapper: &fakeSwapper{}}
	resultNoRelease, err := noReleaseUpdater.Check(context.Background())
	if err != nil || resultNoRelease.Applied || resultNoRelease.Latest != "" || resultNoRelease.Current != "0.1.0" {
		t.Fatalf("no release: result=%+v err=%v", resultNoRelease, err)
	}

	// Tampered archive: checksum must fail and the swapper must NOT be called.
	tampered := append([]byte{}, archive...)
	tampered[0] ^= 0xff
	badClient := &fakeReleaseClient{
		latest: candidate,
		found:  true,
		assets: map[string][]byte{"archive": tampered, "checksums": []byte(checksums)},
	}
	badSwapper := &fakeSwapper{}
	badUpdater := Updater{Current: "0.1.0", Client: badClient, Swapper: badSwapper}
	result3, err := badUpdater.Check(context.Background())
	if err == nil || result3.Applied {
		t.Fatalf("tampered archive: expected refusal, got result=%+v err=%v", result3, err)
	}
	if badSwapper.got != nil {
		t.Fatal("tampered archive was swapped in despite checksum mismatch")
	}
}

func TestUpdaterDoesNotDowngrade(t *testing.T) {
	client := &fakeReleaseClient{
		latest: ReleaseCandidate{Version: "0.1.0"},
		found:  true,
	}
	swapper := &fakeSwapper{}
	u := Updater{Current: "0.2.0", Client: client, Swapper: swapper}

	result, err := u.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Applied {
		t.Fatal("downgrade was applied")
	}
	if len(client.calls) != 0 {
		t.Fatalf("downgrade downloaded assets: %v", client.calls)
	}
}

func TestFileSwapperSwapReplacesInstallPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script executable fixture is Unix-only")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "rfs")
	oldBinary := []byte("#!/bin/sh\necho OLD-BINARY-RAN\n")
	newBinary := []byte("#!/bin/sh\necho NEW-BINARY-RAN\n")
	if err := os.WriteFile(path, oldBinary, 0o750); err != nil {
		t.Fatalf("write old binary: %v", err)
	}

	swapper := FileSwapper{path: path}
	if got := swapper.Path(); got != path {
		t.Fatalf("Path() = %q, want %q", got, path)
	}
	if err := swapper.Swap(newBinary); err != nil {
		t.Fatalf("Swap: %v", err)
	}

	gotNew, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read installed binary: %v", err)
	}
	if !bytes.Equal(gotNew, newBinary) {
		t.Fatalf("installed binary = %q, want %q", gotNew, newBinary)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat installed binary: %v", err)
	}
	if gotMode := info.Mode().Perm(); gotMode != 0o750 {
		t.Fatalf("installed binary mode = %o, want preserved mode 750", gotMode)
	}
	gotOld, err := os.ReadFile(path + ".bak")
	if err != nil {
		t.Fatalf("read backup binary: %v", err)
	}
	if !bytes.Equal(gotOld, oldBinary) {
		t.Fatalf("backup binary = %q, want %q", gotOld, oldBinary)
	}

	out, err := exec.Command(path).CombinedOutput()
	if err != nil {
		t.Fatalf("run installed binary: %v\n%s", err, out)
	}
	if strings.TrimSpace(string(out)) != "NEW-BINARY-RAN" {
		t.Fatalf("installed binary output = %q, want NEW-BINARY-RAN", out)
	}
}

func TestPostSwapExecutablePointsAtBackupOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("/proc/self/exe rename behaviour is Linux-specific")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module helper\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatalf("write helper go.mod: %v", err)
	}
	helperSource := `package main

import (
	"fmt"
	"os"
)

func main() {
	installPath, err := os.Executable()
	if err != nil {
		panic(err)
	}
	bak := installPath + ".bak"
	_ = os.Remove(bak)
	if err := os.Rename(installPath, bak); err != nil {
		panic(err)
	}
	newBinary := []byte("#!/bin/sh\necho NEW-BINARY-RAN\n")
	if err := os.WriteFile(installPath, newBinary, 0o755); err != nil {
		panic(err)
	}
	postSwapPath, err := os.Executable()
	if err != nil {
		panic(err)
	}
	fmt.Println(postSwapPath)
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(helperSource), 0o644); err != nil {
		t.Fatalf("write helper main.go: %v", err)
	}

	helper := filepath.Join(dir, "helper")
	build := exec.Command("go", "build", "-o", helper, ".")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build helper: %v\n%s", err, out)
	}

	out, err := exec.Command(helper).CombinedOutput()
	if err != nil {
		t.Fatalf("run helper: %v\n%s", err, out)
	}
	postSwapPath := strings.TrimSpace(string(out))
	wantPostSwapPath := helper + ".bak"
	if filepath.Clean(postSwapPath) != filepath.Clean(wantPostSwapPath) {
		t.Fatalf("post-swap os.Executable() = %q, want %q", postSwapPath, wantPostSwapPath)
	}

	out, err = exec.Command(helper).CombinedOutput()
	if err != nil {
		t.Fatalf("run install path after swap: %v\n%s", err, out)
	}
	if strings.TrimSpace(string(out)) != "NEW-BINARY-RAN" {
		t.Fatalf("install path output = %q, want NEW-BINARY-RAN", out)
	}
}
