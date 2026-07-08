package sqlddl_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// New() must default to the PostgreSQL dialect: New() and
// New(WithDialect(Postgres())) must be indistinguishable on a real-world
// fixture (Pagila) — the identity/no-op transform the design promises so the
// PG no-regression guard holds regardless of which spelling a caller uses.
func TestNew_DefaultsToPostgres(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("testdata", "pagila_excerpt.sql"))
	if err != nil {
		t.Fatal(err)
	}
	src := []providers.SourceFile{{Path: "pagila.sql", Content: content}}

	sDefault, err := sqlddl.New().ParseSchema(src)
	if err != nil {
		t.Fatalf("default ParseSchema: %v", err)
	}
	sExplicit, err := sqlddl.New(sqlddl.WithDialect(sqlddl.Postgres())).ParseSchema(src)
	if err != nil {
		t.Fatalf("explicit Postgres ParseSchema: %v", err)
	}

	gotDefault, err := json.Marshal(sDefault)
	if err != nil {
		t.Fatalf("marshal default schema: %v", err)
	}
	gotExplicit, err := json.Marshal(sExplicit)
	if err != nil {
		t.Fatalf("marshal explicit schema: %v", err)
	}
	if string(gotDefault) != string(gotExplicit) {
		t.Errorf("New() and New(WithDialect(Postgres())) produced different schemas:\ndefault:  %s\nexplicit: %s", gotDefault, gotExplicit)
	}
}
