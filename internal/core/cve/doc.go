// Package cve checks project dependencies against known vulnerabilities via
// OSV.dev (PRD section 18, RF-09). codefit keeps NO vulnerability database of
// its own: on each scan it queries OSV.dev (free, no API key, aggregating the
// GitHub Advisory Database, Linux distro feeds and more), so the data is always
// fresh and maintained by infrastructure the whole world already runs.
//
// Status: SKELETON — implementation pending (RF-09). This declares the
// [Dependency] and [Vulnerability] types and the [Client] contract only; the
// OSV.dev HTTP client and the dependency-manifest parsing are not implemented yet.
package cve
