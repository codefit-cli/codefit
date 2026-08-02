// Package cve checks project dependencies against known vulnerabilities via
// OSV.dev (PRD section 18, RF-09). codefit keeps NO vulnerability database of
// its own: on each scan it queries OSV.dev (free, no API key, aggregating the
// GitHub Advisory Database, Linux distro feeds and more), so the data is always
// fresh and maintained by infrastructure the whole world already runs.
//
// Status: BUILT (RF-09). The OSV.dev HTTP client and the dependency-manifest
// parsing are implemented and reachable through the codefit-check-cves tool:
// exact versions are read from package-lock.json and go.mod, never guessed from
// a package.json range — a manifest present without its lockfile is reported as
// a note instead.
package cve
