package cve

import (
	"fmt"
	"os"
	"path/filepath"
)

// ManifestScan is the result of reading a project's dependency manifests: the
// exact-versioned dependencies found, plus honest notes about anything that could
// NOT be resolved (a manifest present without its lockfile, or no manifest at
// all), so the caller never mistakes "no dependencies parsed" for "no
// dependencies present".
type ManifestScan struct {
	Dependencies []Dependency
	Notes        []string
}

// ParseManifests reads the supported dependency manifests at the project root and
// returns the exact-versioned dependencies across ecosystems. Version resolution
// is from LOCKFILES / go.mod only (RF-09 decision A): the ranges in package.json
// are never resolved — codefit reports what is pinned, not a guess. A manifest
// present but unparseable is a loud error (never silently dropped); an absent
// manifest is fine and recorded as a note.
//
// Known limits (declared): only package-lock.json (npm) and go.mod (Go) are read
// at the project root — yarn/pnpm lockfiles and nested/monorepo manifests are not
// yet covered.
func ParseManifests(root string) (ManifestScan, error) {
	var scan ManifestScan

	// npm — exact versions live in the lockfile, never in package.json's ranges.
	lockPath := filepath.Join(root, "package-lock.json")
	switch {
	case fileExists(lockPath):
		deps, err := parseManifestFile(lockPath, parseNpmLock)
		if err != nil {
			return ManifestScan{}, err
		}
		scan.Dependencies = append(scan.Dependencies, deps...)
	case fileExists(filepath.Join(root, "package.json")):
		scan.Notes = append(scan.Notes,
			"package.json found but no package-lock.json: codefit reports only EXACT versions and does not resolve ranges, so npm dependencies cannot be checked without a lockfile — run `npm install` to generate one.")
	}

	// Go — go.mod pins exact versions for the whole graph (Go 1.17+).
	goModPath := filepath.Join(root, "go.mod")
	if fileExists(goModPath) {
		deps, err := parseManifestFile(goModPath, parseGoMod)
		if err != nil {
			return ManifestScan{}, err
		}
		scan.Dependencies = append(scan.Dependencies, deps...)
	}

	if len(scan.Dependencies) == 0 && len(scan.Notes) == 0 {
		scan.Notes = append(scan.Notes,
			"no supported dependency manifest found at the project root (looked for package-lock.json and go.mod).")
	}
	return scan, nil
}

// parseManifestFile reads a manifest and parses it, wrapping any read/parse error
// with the path — a present-but-unreadable manifest is surfaced loudly.
func parseManifestFile(path string, parse func([]byte) ([]Dependency, error)) ([]Dependency, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	deps, err := parse(data)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return deps, nil
}

// fileExists reports whether path is an existing regular file.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
