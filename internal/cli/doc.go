// Package cli wires the codefit command tree with cobra: the root command and
// its global flags, plus every subcommand (init, scan, bench, review, run,
// baseline, report, mcp serve, auth, set, status).
//
// Skeleton: commands declare their flags and help text but carry no business
// logic — every RunE is a stub until the engine is built.
package cli
