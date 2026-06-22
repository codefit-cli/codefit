package cve

import "context"

// Dependency is one parsed project dependency (from go.mod, package.json, ...)
// at an exact version, in its ecosystem (OSV.dev uses the ecosystem to scope the
// query, e.g. "Go", "npm", "PyPI").
type Dependency struct {
	Name      string
	Version   string
	Ecosystem string
}

// Vulnerability is a known vulnerability OSV.dev reports for a dependency.
type Vulnerability struct {
	ID         string // e.g. "GHSA-..." or "CVE-..."
	Summary    string
	Severity   string // CVSS-derived
	FixedIn    string // first fixed version, if known
	References []string
}

// Client queries OSV.dev for the vulnerabilities affecting a set of
// dependencies.
//
// Skeleton: no implementation yet (Fase 1).
type Client interface {
	Query(ctx context.Context, deps []Dependency) (map[string][]Vulnerability, error)
}
