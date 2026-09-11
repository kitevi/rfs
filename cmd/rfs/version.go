package main

import (
	"runtime"

	"github.com/kitevi/rfs/internal/rfs"
)

// Build metadata for the running rfs binary. version/commit/commitDate/buildDate
// are injected at release time via -ldflags (see .goreleaser.yaml); the go
// version, OS, and arch come from the runtime, so they need no injection.
// Defaults make local `go run`/`go install` builds self-identify as "dev" so
// they skip self-update (see ADR 0004).
var (
	version    = "dev"
	commit     = "none"
	commitDate = "unknown"
	buildDate  = "unknown"
)

// buildInfo is the single description of this build: the -version flag, the
// startup log, and the HTML page footer all render buildInfo.String(), so an
// operator sees the same identifier in every place. The self-update gate
// reads the bare `version` var above (the timestamp it compares), not this.
var buildInfo = rfs.BuildInfo{
	Version:    version,
	Commit:     commit,
	CommitDate: commitDate,
	BuildDate:  buildDate,
	GoVersion:  runtime.Version(),
	GOOS:       runtime.GOOS,
	GOARCH:     runtime.GOARCH,
}
