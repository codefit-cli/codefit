package registry

import (
	"os"
	"path/filepath"

	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/golang"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

// Exposure declares which resolvers currently admit a registered language,
// independent of Capability (what the provider implements). Each field names
// the consumer it gates:
//   - SecurityScan: internal/mcp's providerForLanguage (codefit-scan-security,
//     codefit-scan-endpoint, codefit-scan-all's security bucket, and
//     codefit-coverage, which all resolve through it).
//   - SurfaceTools: internal/mcp/surface.go's providerFor (the
//     codefit-surface-* tools).
//   - InitDetect: internal/scaffold's Detect (codefit init).
type Exposure struct {
	SecurityScan bool
	SurfaceTools bool
	InitDetect   bool
}

// Entry is one registered language. Deliberately has NO extensions field —
// extensions are read from New(nil).FileExtensions() (ByExtension, below), so
// they can never drift from what the provider itself declares.
type Entry struct {
	Canonical   string   // "typescript"
	Aliases     []string // "ts", "tsx"
	MarkerFiles []string // ordered within the entry: e.g. "package.json", "tsconfig.json"
	New         func(authzHelpers []string) providers.LanguageProvider
	Exposure    Exposure
}

// table is the ONE ordered registry. Order matters for ByMarkerFile: go.mod
// wins over package.json in a polyglot root, matching
// scaffold/detect.go:68-84's documented priority, so "go" is listed first.
var table = []Entry{
	{
		Canonical:   "go",
		MarkerFiles: []string{"go.mod"},
		New:         func(_ []string) providers.LanguageProvider { return golang.New() },
		Exposure:    Exposure{SecurityScan: false, SurfaceTools: false, InitDetect: true},
	},
	{
		Canonical:   "typescript",
		Aliases:     []string{"ts", "tsx"},
		MarkerFiles: []string{"package.json", "tsconfig.json"},
		New: func(authzHelpers []string) providers.LanguageProvider {
			return typescript.New(typescript.WithAuthzHelpers(authzHelpers))
		},
		Exposure: Exposure{SecurityScan: true, SurfaceTools: true, InitDetect: true},
	},
}

// All returns every registered entry, in table order. Callers get a copy —
// the table itself is never mutated after init.
func All() []Entry {
	out := make([]Entry, len(table))
	copy(out, table)
	return out
}

// ByName resolves an entry by canonical name or any of its aliases.
func ByName(name string) (Entry, bool) {
	for _, e := range table {
		if e.Canonical == name {
			return e, true
		}
		for _, a := range e.Aliases {
			if a == name {
				return e, true
			}
		}
	}
	return Entry{}, false
}

// ByExtension resolves an entry by file extension, read from each entry's
// real provider (New(nil).FileExtensions()) — never a stored table field, so
// this query cannot drift from what the provider itself declares.
func ByExtension(ext string) (Entry, bool) {
	for _, e := range table {
		for _, x := range e.New(nil).FileExtensions() {
			if x == ext {
				return e, true
			}
		}
	}
	return Entry{}, false
}

// ByMarkerFile resolves the first entry (in table order) with a marker file
// directly under root. Table order is the priority: in a polyglot root with
// both go.mod and package.json, "go" (listed first) wins.
func ByMarkerFile(root string) (Entry, bool) {
	for _, e := range table {
		for _, m := range e.MarkerFiles {
			if _, err := os.Stat(filepath.Join(root, m)); err == nil {
				return e, true
			}
		}
	}
	return Entry{}, false
}

// ExposedForSecurity returns the entries whose Exposure.SecurityScan is true,
// in table order — the set internal/mcp's providerForLanguage/
// SupportedLanguageNames resolve a security provider for.
func ExposedForSecurity() []Entry {
	return filterExposure(func(e Entry) bool { return e.Exposure.SecurityScan })
}

// ExposedForSurfaceTools returns the entries whose Exposure.SurfaceTools is
// true, in table order — the set internal/mcp/surface.go's providerFor
// resolves for the codefit-surface-* tools.
func ExposedForSurfaceTools() []Entry {
	return filterExposure(func(e Entry) bool { return e.Exposure.SurfaceTools })
}

func filterExposure(pred func(Entry) bool) []Entry {
	var out []Entry
	for _, e := range table {
		if pred(e) {
			out = append(out, e)
		}
	}
	return out
}
