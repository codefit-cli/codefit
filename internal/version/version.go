// Package version holds the build-time identity of the codefit binary. The
// values are overridden at build time via -ldflags (see the Makefile and
// .goreleaser.yaml); the defaults apply to plain `go build`/`go run`.
package version

var (
	// Version is the semantic version (e.g. "v0.1.0").
	Version = "0.1.0-dev"
	// Commit is the short git commit the binary was built from.
	Commit = "none"
	// BuildDate is the RFC3339 build timestamp.
	BuildDate = "unknown"
)
