package cve

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// npmLock is the subset of package-lock.json codefit reads. lockfileVersion 2/3
// use the `packages` map (keyed by install path); version 1 uses a nested
// `dependencies` map. Both pin EXACT versions — the only honest source, since
// package.json carries ranges, which codefit does not resolve (RF-09).
type npmLock struct {
	Packages map[string]struct {
		Version string `json:"version"`
		Name    string `json:"name"` // present for aliased installs
	} `json:"packages"`
	Dependencies map[string]npmLockDep `json:"dependencies"`
}

// npmLockDep is a node of the lockfileVersion-1 nested dependency tree.
type npmLockDep struct {
	Version      string                `json:"version"`
	Dependencies map[string]npmLockDep `json:"dependencies"`
}

// parseNpmLock extracts the exact-versioned npm dependencies from a
// package-lock.json (lockfileVersion 1, 2, or 3). The root project (the "" entry)
// is skipped; duplicates across the tree are de-duplicated by name+version; the
// result is sorted for a deterministic output.
func parseNpmLock(data []byte) ([]Dependency, error) {
	var lock npmLock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, fmt.Errorf("parsing package-lock.json: %w", err)
	}

	seen := map[string]bool{}
	var out []Dependency
	add := func(name, version string) {
		if name == "" || version == "" {
			return
		}
		key := name + "@" + version
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, Dependency{Name: name, Version: version, Ecosystem: "npm"})
	}

	// lockfileVersion 2/3: the `packages` map, keyed by install path.
	for path, pkg := range lock.Packages {
		if path == "" { // the root project, not a dependency
			continue
		}
		name := pkg.Name
		if name == "" {
			name = npmNameFromPath(path)
		}
		add(name, pkg.Version)
	}

	// lockfileVersion 1 (no `packages`): the nested `dependencies` tree.
	if len(lock.Packages) == 0 {
		var walk func(deps map[string]npmLockDep)
		walk = func(deps map[string]npmLockDep) {
			for name, d := range deps {
				add(name, d.Version)
				walk(d.Dependencies)
			}
		}
		walk(lock.Dependencies)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Version < out[j].Version
	})
	return out, nil
}

// npmNameFromPath returns the package name from a package-lock `packages` key:
// the segment after the LAST "node_modules/", so a nested
// "node_modules/express/node_modules/cookie" resolves to "cookie".
func npmNameFromPath(path string) string {
	const marker = "node_modules/"
	if i := strings.LastIndex(path, marker); i >= 0 {
		return path[i+len(marker):]
	}
	return path
}
