package db

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSQL(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("SELECT 1;"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func base(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = filepath.Base(p)
	}
	return out
}

func TestFlywayOrder_NumericNotLexical(t *testing.T) {
	dir := t.TempDir()
	writeSQL(t, dir, "V10__b.sql", "V2__a.sql", "V1__x.sql")
	got, err := flywayOrderedSQL(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"V1__x.sql", "V2__a.sql", "V10__b.sql"} // numeric, not lexical (V10 last)
	if b := base(got); !equalSlice(b, want) {
		t.Errorf("order = %v, want %v (numeric Flyway version, not lexical)", b, want)
	}
}

// AJUSTE 2: dotted versions (V1.1) are a DECLARED LIMIT — they do not match the
// integer Flyway pattern and fall to the lexical bucket AFTER all versioned files.
// This locks the behavior as a contract, not an accident.
func TestFlywayOrder_DottedVersion_Declared(t *testing.T) {
	dir := t.TempDir()
	writeSQL(t, dir, "V2__a.sql", "V1.1__dotted.sql", "V10__b.sql")
	got, err := flywayOrderedSQL(dir)
	if err != nil {
		t.Fatal(err)
	}
	b := base(got)
	// The two integer-versioned files come first, in numeric order; the dotted one
	// falls to the lexical bucket at the end (not ordered by its numeric version).
	want := []string{"V2__a.sql", "V10__b.sql", "V1.1__dotted.sql"}
	if !equalSlice(b, want) {
		t.Errorf("order = %v, want %v (dotted version is a declared limit → lexical bucket last)", b, want)
	}
}

func TestFlywayOrder_IgnoresNonSQL(t *testing.T) {
	dir := t.TempDir()
	writeSQL(t, dir, "V1__a.sql")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := flywayOrderedSQL(dir)
	if err != nil {
		t.Fatal(err)
	}
	if b := base(got); !equalSlice(b, []string{"V1__a.sql"}) {
		t.Errorf("order = %v, want only the .sql file", b)
	}
}

func equalSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
