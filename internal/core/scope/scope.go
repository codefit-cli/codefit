package scope

import (
	"path"
	"sort"
	"strings"
)

// Scope is the set of project-relative files an audit pass may examine. Build it
// with [Full] or [Of]; the zero value includes nothing (see the package doc).
type Scope struct {
	full  bool
	files map[string]bool
}

// Full returns the scope that includes every file — a complete audit.
func Full() Scope { return Scope{full: true} }

// Of returns the scope of exactly these project-relative paths, canonicalized.
// An absent or empty list (or one holding only blanks) returns [Full]: an empty
// scope is never read as "audit nothing".
func Of(rels []string) Scope {
	files := make(map[string]bool, len(rels))
	for _, r := range rels {
		if strings.TrimSpace(r) == "" {
			continue
		}
		files[Canon(r)] = true
	}
	if len(files) == 0 {
		return Full()
	}
	return Scope{files: files}
}

// IsFull reports whether the scope includes every file.
func (s Scope) IsFull() bool { return s.full }

// Narrows reports whether the scope actually restricts anything. It is false for
// [Full] AND for the zero value — the question a WALK must ask, so an
// unconfigured scope widens the audit instead of blanking it. Do not confuse it
// with [Scope.Includes], whose zero-value answer is deliberately the opposite.
func (s Scope) Narrows() bool { return !s.full && len(s.files) > 0 }

// Includes reports whether rel is in the scope. rel is canonicalized on lookup,
// exactly as it was on construction, so a caller's separator or "./" prefix
// cannot make the same file miss itself.
func (s Scope) Includes(rel string) bool {
	if s.full {
		return true
	}
	return s.files[Canon(rel)]
}

// Files returns the scoped paths, canonical and sorted — nil when the scope is
// full (there is no finite list of "everything"). The result is a copy.
func (s Scope) Files() []string {
	if s.full || len(s.files) == 0 {
		return nil
	}
	out := make([]string, 0, len(s.files))
	for f := range s.files {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// Canon is the canonical form of a project-relative path, and the SINGLE
// definition of it: separators normalized to "/", then cleaned. A caller that
// must compare a path against a scope (a configured schema path, say) uses this
// rather than re-deriving the rule.
//
// It is deliberately platform-INDEPENDENT — path.Clean over slash-normalized
// text, not filepath.Clean. The paths a scope holds are project-relative
// identifiers that travel between an agent, a committed baseline and a
// repository checked out on either OS; resolving `src\a.ts` only when codefit
// happens to be running on Windows would reintroduce exactly the separator drift
// v0.2.6 paid to remove.
func Canon(p string) string {
	return path.Clean(strings.ReplaceAll(p, `\`, "/"))
}
