package scope_test

import (
	"reflect"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/scope"
)

// A full scope includes everything, declares itself full, narrows nothing, and
// lists no files (nil — there is no finite list of "everything").
func TestFull_IncludesEverything(t *testing.T) {
	s := scope.Full()
	if !s.IsFull() {
		t.Error("Full().IsFull() = false, want true")
	}
	if s.Narrows() {
		t.Error("Full().Narrows() = true, want false — a full scope restricts nothing")
	}
	for _, p := range []string{"src/a.ts", "anything/at/all.go", ""} {
		if !s.Includes(p) {
			t.Errorf("Full().Includes(%q) = false, want true", p)
		}
	}
	if s.Files() != nil {
		t.Errorf("Full().Files() = %v, want nil", s.Files())
	}
}

// The zero value includes NOTHING. This is deliberate: it is the fail-safe for
// baseline.Diff's prune guard — a caller that forgets to pass a scope
// under-reports (prunes nothing) instead of corrupting the baseline.
func TestZeroValue_IncludesNothing(t *testing.T) {
	var s scope.Scope
	if s.IsFull() {
		t.Error("Scope{}.IsFull() = true, want false")
	}
	if s.Includes("src/a.ts") {
		t.Error("Scope{}.Includes(\"src/a.ts\") = true, want false — the zero value includes nothing")
	}
	if s.Narrows() {
		t.Error("Scope{}.Narrows() = true, want false — an unset scope must not silence a walk")
	}
	if len(s.Files()) != 0 {
		t.Errorf("Scope{}.Files() = %v, want empty", s.Files())
	}
}

// An absent or empty list means FULL, never "audit nothing". The fail-safe
// direction for a scope is to audit MORE.
func TestOf_EmptyMeansFull(t *testing.T) {
	for name, in := range map[string][]string{"nil": nil, "empty": {}, "blank entries": {"", "   "}} {
		s := scope.Of(in)
		if !s.IsFull() {
			t.Errorf("Of(%s).IsFull() = false, want true (empty means full)", name)
		}
		if !s.Includes("whatever.ts") {
			t.Errorf("Of(%s) must include everything", name)
		}
	}
}

func TestOf_IncludesExactlyTheGivenFiles(t *testing.T) {
	s := scope.Of([]string{"src/b.ts", "src/a.ts"})
	if s.IsFull() {
		t.Error("Of(files).IsFull() = true, want false")
	}
	if !s.Narrows() {
		t.Error("Of(files).Narrows() = false, want true")
	}
	if !s.Includes("src/a.ts") || !s.Includes("src/b.ts") {
		t.Error("Of(files) must include the given files")
	}
	if s.Includes("src/c.ts") {
		t.Error("Of(files) must NOT include a file it was not given")
	}
	if got, want := s.Files(), []string{"src/a.ts", "src/b.ts"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Files() = %v, want %v (sorted)", got, want)
	}
}

// Test contract item 7: canonicalization on BOTH sides. A Windows separator, a
// leading "./" and the plain relative path are the SAME file — on every platform,
// because a caller's path is a project-relative path, not a host path.
func TestCanonicalization_SameFileManySpellings(t *testing.T) {
	spellings := []string{`src\a.ts`, "./src/a.ts", "src/a.ts", "src/./a.ts", `.\src\a.ts`, "src/sub/../a.ts"}
	for _, constructed := range spellings {
		s := scope.Of([]string{constructed})
		for _, looked := range spellings {
			if !s.Includes(looked) {
				t.Errorf("Of(%q).Includes(%q) = false, want true — same file, different spelling", constructed, looked)
			}
		}
		if got, want := s.Files(), []string{"src/a.ts"}; !reflect.DeepEqual(got, want) {
			t.Errorf("Of(%q).Files() = %v, want %v (canonical)", constructed, got, want)
		}
	}
}

// Canon is the single place the canonical form is defined, exported so callers
// that must compare a path to a scope (e.g. a configured schema path against the
// scoped files) use the SAME rule instead of re-deriving it.
func TestCanon(t *testing.T) {
	cases := map[string]string{
		`src\a.ts`:    "src/a.ts",
		"./src/a.ts":  "src/a.ts",
		"src/a.ts":    "src/a.ts",
		`prisma\`:     "prisma",
		"migrations/": "migrations",
	}
	for in, want := range cases {
		if got := scope.Canon(in); got != want {
			t.Errorf("Canon(%q) = %q, want %q", in, got, want)
		}
	}
}

// Duplicates collapse: the same file spelled twice is one file.
func TestOf_Deduplicates(t *testing.T) {
	s := scope.Of([]string{"src/a.ts", `src\a.ts`, "./src/a.ts"})
	if got := s.Files(); len(got) != 1 {
		t.Errorf("Files() = %v, want exactly one file", got)
	}
}

// Files() returns a copy: a caller cannot mutate the scope through it.
func TestFiles_IsACopy(t *testing.T) {
	s := scope.Of([]string{"src/a.ts"})
	got := s.Files()
	got[0] = "hacked.ts"
	if !s.Includes("src/a.ts") || s.Includes("hacked.ts") {
		t.Error("mutating the slice returned by Files() must not change the scope")
	}
}
