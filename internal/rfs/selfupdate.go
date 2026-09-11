package rfs

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// extractBinaryFromArchive reads a verified goreleaser tar.gz archive and
// returns the bytes of the single regular "rfs" binary inside it. Both the
// archive and the extracted binary have independent size limits so malformed
// release artifacts cannot exhaust memory or decompression work.
func extractBinaryFromArchive(archive []byte) ([]byte, error) {
	gr, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("self-update: read archive gzip: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(&sizeLimitedReader{reader: gr, remaining: maxExtractedArchiveBytes})
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("self-update: archive has no rfs entry")
		}
		if err != nil {
			return nil, fmt.Errorf("self-update: read archive: %w", err)
		}
		if hdr.Name != "rfs" {
			continue
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			return nil, fmt.Errorf("self-update: rfs entry is not a regular file")
		}
		if hdr.Size <= 0 || hdr.Size > maxExtractedBinaryBytes {
			return nil, fmt.Errorf("self-update: rfs entry has invalid size %d", hdr.Size)
		}
		binary, err := io.ReadAll(io.LimitReader(tr, hdr.Size+1))
		if err != nil {
			return nil, fmt.Errorf("self-update: read rfs entry: %w", err)
		}
		if int64(len(binary)) != hdr.Size {
			return nil, fmt.Errorf("self-update: rfs entry size = %d, want %d", len(binary), hdr.Size)
		}
		return binary, nil
	}
}

type sizeLimitedReader struct {
	reader    io.Reader
	remaining int64
}

func (r *sizeLimitedReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, fmt.Errorf("self-update: archive exceeds %d byte extraction limit", maxExtractedArchiveBytes)
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.reader.Read(p)
	r.remaining -= int64(n)
	return n, err
}

// ReleaseCandidate is the platform-specific archive selected by the release
// adapter. Its IDs are opaque to rfs and are used only to download the archive
// and checksums asset through the provider.
type ReleaseCandidate struct {
	Version     string
	ArchiveName string
	ArchiveID   int64
	ChecksumsID int64
	// providerRelease is owned by the ReleaseClient implementation. It lets the
	// adapter retain provider-specific state without leaking it into Updater.
	providerRelease any
}

// ReleaseClient is the seam between the self-update workflow and its release
// provider. The production implementation is backed by go-selfupdate; tests
// can exercise the workflow without making network requests.
type ReleaseClient interface {
	Latest(context.Context) (ReleaseCandidate, bool, error)
	Download(context.Context, ReleaseCandidate, int64) ([]byte, error)
}

// BinarySwapper atomically replaces the running binary with newBinary. The
// real implementation renames the old binary aside (.bak) and the new into
// place; tests fake it to observe what would have been swapped in.
type BinarySwapper interface {
	Swap(newBinary []byte) error
}

// Updater runs one self-update check: fetch the latest release, and if it is
// newer than Current, download the matching archive and checksums.txt,
// verify the archive, extract the rfs binary, and swap it in. Check returns a
// result describing the observed latest release and whether a binary was
// swapped.
type Updater struct {
	Current string
	Client  ReleaseClient
	Swapper BinarySwapper
}

// UpdateCheckResult describes the outcome of one self-update check.
type UpdateCheckResult struct {
	Current string
	Latest  string
	Applied bool
}

func (u Updater) Check(ctx context.Context) (UpdateCheckResult, error) {
	result := UpdateCheckResult{Current: u.Current}
	if u.Client == nil {
		return result, errors.New("self-update: release client is nil")
	}

	candidate, found, err := u.Client.Latest(ctx)
	if err != nil {
		return result, fmt.Errorf("self-update: fetch latest release: %w", err)
	}
	if !found {
		return result, nil
	}
	result.Latest = candidate.Version
	if !isNewerVersion(u.Current, candidate.Version) {
		return result, nil
	}
	if candidate.ArchiveName == "" || candidate.ArchiveID <= 0 || candidate.ChecksumsID <= 0 {
		return result, fmt.Errorf("self-update: release %s has incomplete asset metadata", candidate.Version)
	}

	checksums, err := u.Client.Download(ctx, candidate, candidate.ChecksumsID)
	if err != nil {
		return result, fmt.Errorf("self-update: download checksums: %w", err)
	}
	archive, err := u.Client.Download(ctx, candidate, candidate.ArchiveID)
	if err != nil {
		return result, fmt.Errorf("self-update: download archive: %w", err)
	}
	if err := validateReleaseChecksum(candidate.ArchiveName, archive, checksums); err != nil {
		return result, fmt.Errorf("self-update: verify archive: %w", err)
	}

	binary, err := extractBinaryFromArchive(archive)
	if err != nil {
		return result, err
	}
	if u.Swapper == nil {
		return result, errors.New("self-update: binary swapper is nil")
	}
	if err := u.Swapper.Swap(binary); err != nil {
		return result, fmt.Errorf("self-update: swap binary: %w", err)
	}
	result.Applied = true
	return result, nil
}

const (
	selfUpdateRepo             = "kitevi/rfs"
	defaultUpdateCheckInterval = 10 * time.Minute
	defaultUpdateCheckTimeout  = 30 * time.Second
)

// UpdateChecker performs one self-update check. Updater is the production
// adapter; tests use a fake to control monitor outcomes.
type UpdateChecker interface {
	Check(context.Context) (UpdateCheckResult, error)
}

// UpdateMonitorConfig controls the cadence and deadline of independent
// self-update checks. Zero values use the production defaults.
type UpdateMonitorConfig struct {
	CheckInterval time.Duration
	CheckTimeout  time.Duration
}

// UpdateCheckEvent is one completed self-update check. ReexecPath is set by
// the production monitor and names the newly installed binary after Applied.
type UpdateCheckEvent struct {
	Result     UpdateCheckResult
	Err        error
	ReexecPath string
}

// UpdateMonitor owns serial, independent self-update checks. Run checks
// immediately, then at CheckInterval, and stops after applying an update so a
// running process can never swap its binary twice.
type UpdateMonitor struct {
	checker    UpdateChecker
	config     UpdateMonitorConfig
	reexecPath string
}

// NewUpdateMonitor creates the production self-update monitor. It hides the
// GitHub, platform, downloader, and filesystem adapters from callers.
func NewUpdateMonitor(current string, config UpdateMonitorConfig) (*UpdateMonitor, error) {
	client, err := newSelfUpdateClient()
	if err != nil {
		return nil, err
	}
	swapper, err := NewFileSwapper()
	if err != nil {
		return nil, err
	}
	monitor := newUpdateMonitor(Updater{
		Current: current,
		Client:  client,
		Swapper: swapper,
	}, config)
	monitor.reexecPath = swapper.Path()
	return &monitor, nil
}

func newUpdateMonitor(checker UpdateChecker, config UpdateMonitorConfig) UpdateMonitor {
	return UpdateMonitor{checker: checker, config: config}
}

// Run returns checks from a goroutine until ctx is cancelled or an update is
// applied. Each check has its own deadline, so it cannot consume a source-poll
// deadline or overlap another check.
func (m UpdateMonitor) Run(ctx context.Context) <-chan UpdateCheckEvent {
	events := make(chan UpdateCheckEvent)
	go func() {
		defer close(events)
		timer := time.NewTimer(0)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}

			checkCtx, cancel := context.WithTimeout(ctx, m.checkTimeout())
			result, err := m.checker.Check(checkCtx)
			cancel()
			if ctx.Err() != nil {
				return
			}
			event := UpdateCheckEvent{Result: result, Err: err, ReexecPath: m.reexecPath}
			select {
			case <-ctx.Done():
				return
			case events <- event:
			}
			if result.Applied {
				return
			}
			timer.Reset(m.checkInterval())
		}
	}()
	return events
}

func (m UpdateMonitor) checkInterval() time.Duration {
	if m.config.CheckInterval <= 0 {
		return defaultUpdateCheckInterval
	}
	return m.config.CheckInterval
}

func (m UpdateMonitor) checkTimeout() time.Duration {
	if m.config.CheckTimeout <= 0 {
		return defaultUpdateCheckTimeout
	}
	return m.config.CheckTimeout
}

// FileSwapper replaces the running binary on disk atomically: the old binary
// is renamed aside to .bak (manual rollback per ADR 0004) and the new bytes
// are written to a temp file in the same directory then renamed into place.
type FileSwapper struct {
	// path is the running binary's path (os.Executable()). Overridden in tests.
	path string
}

func NewFileSwapper() (FileSwapper, error) {
	p, err := os.Executable()
	if err != nil {
		return FileSwapper{}, fmt.Errorf("self-update: locate running binary: %w", err)
	}
	return FileSwapper{path: p}, nil
}

// Path returns the binary install path captured before any swap. Use this path
// for re-exec after Swap: on Linux, a fresh os.Executable() after the rename can
// point at the .bak backup (the old binary), not the newly installed file.
func (s FileSwapper) Path() string { return s.path }

func (s FileSwapper) Swap(newBinary []byte) error {
	installed, err := os.Stat(s.path)
	if err != nil {
		return fmt.Errorf("stat installed binary: %w", err)
	}
	if !installed.Mode().IsRegular() {
		return fmt.Errorf("installed binary is not a regular file")
	}

	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".rfs-new-")
	if err != nil {
		return fmt.Errorf("create temp binary: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once renamed away
	if _, err := tmp.Write(newBinary); err != nil {
		tmp.Close()
		return fmt.Errorf("write new binary: %w", err)
	}
	if err := tmp.Chmod(installed.Mode().Perm()); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod new binary: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync new binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp binary: %w", err)
	}

	bak := s.path + ".bak"
	_ = os.Remove(bak) // best-effort: clear any previous backup
	if err := os.Rename(s.path, bak); err != nil {
		return fmt.Errorf("set aside old binary: %w", err)
	}
	if err := syncDirectory(dir); err != nil {
		_ = os.Rename(bak, s.path)
		return fmt.Errorf("sync backup rename: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		// Try to restore the old binary so the box isn't left without one.
		_ = os.Rename(bak, s.path)
		return fmt.Errorf("install new binary: %w", err)
	}
	if err := syncDirectory(dir); err != nil {
		return fmt.Errorf("sync installed binary: %w", err)
	}
	return nil
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

const (
	maxAssetBytes            int64 = 256 << 20 // generous cap on a compressed release asset
	maxExtractedArchiveBytes int64 = 256 << 20 // cap all decompressed tar content
	maxExtractedBinaryBytes  int64 = 128 << 20 // cap the executable within that archive
)
