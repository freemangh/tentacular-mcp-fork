// Package version holds build-time version info injected by GoReleaser ldflags.
package version

// Set by GoReleaser ldflags.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)
