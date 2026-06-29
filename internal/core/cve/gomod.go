package cve

import (
	"bufio"
	"bytes"
	"fmt"
	"sort"
	"strings"
)

// parseGoMod extracts the exact-versioned module dependencies from a go.mod. It
// reads the `require` directives (block and single-line forms), including
// // indirect entries — Go pins every version via MVS, so go.mod is the exact,
// complete graph for Go 1.17+ (graph pruning lists all deps). The module line is
// skipped, and `replace`/`exclude`/`retract` are ignored.
//
// Known limits (declared, not silent): a `replace` redirecting to a different
// version is NOT applied — the required version is reported; and a pre-1.17
// go.mod may under-list indirect deps (resolving the full graph would need the Go
// toolchain at runtime, out of scope for a static read).
func parseGoMod(data []byte) ([]Dependency, error) {
	seen := map[string]bool{}
	var out []Dependency
	add := func(path, version string) {
		if path == "" || version == "" {
			return
		}
		key := path + "@" + version
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, Dependency{Name: path, Version: version, Ecosystem: "Go"})
	}

	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	inRequire := false
	for sc.Scan() {
		line := sc.Text()
		// Module paths never contain "//"; cut any line comment (e.g. "// indirect").
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if inRequire {
			if line == ")" {
				inRequire = false
				continue
			}
			if path, ver, ok := splitModVer(line); ok {
				add(path, ver)
			}
			continue
		}

		switch {
		case line == "require (":
			inRequire = true
		case strings.HasPrefix(line, "require "):
			rest := strings.TrimSpace(strings.TrimPrefix(line, "require "))
			if rest == "(" {
				inRequire = true
				continue
			}
			if path, ver, ok := splitModVer(rest); ok {
				add(path, ver)
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading go.mod: %w", err)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Version < out[j].Version
	})
	return out, nil
}

// splitModVer splits a "module/path vVERSION" require entry into its path and
// version, ignoring any trailing tokens.
func splitModVer(s string) (path, version string, ok bool) {
	f := strings.Fields(s)
	if len(f) < 2 {
		return "", "", false
	}
	return f[0], f[1], true
}
