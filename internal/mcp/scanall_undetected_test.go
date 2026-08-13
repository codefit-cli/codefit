package mcp_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/mcp"
)

// `codefit init` on a root where no marker file resolves a provider writes
// project.language: "undetected" and NO database section, and it declares two
// consequences to the developer's face — in the init report, in the generated
// config and in the README. This file makes both of those claims executable.
//
// It is deliberately separate from scanall_dbonly_test.go, which locks P0-5's
// "the DB dimension does not need a language" property with an unregistered
// REAL language and must keep doing exactly that, unchanged. What is asserted
// here is narrower and newer: the sentinel codefit itself writes behaves the
// same way, so nothing codefit tells that developer is a guess.

const undetectedYAMLWithSchema = `version: "1"
project:
  name: t
  language: "` + config.LanguageUndetected + `"
database:
  type: postgresql
  schema_paths:
    - db/schema.sql
`

const undetectedYAMLNoSchema = `version: "1"
project:
  name: t
  language: "` + config.LanguageUndetected + `"
`

// TestHandleScanAll_UndetectedWithSchema_AuditsDB is the promise `codefit init`
// makes to a project it could not identify: add database.schema_paths and the DB
// dimension runs. The schema parser resolves by the INPUT's shape, never by the
// language, so the sentinel does not stand in its way.
func TestHandleScanAll_UndetectedWithSchema_AuditsDB(t *testing.T) {
	root := writeProj(t, map[string]string{
		".codefit.yaml": undetectedYAMLWithSchema,
		"db/schema.sql": unresolvedNoPKSchema,
	})

	resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: config.LanguageUndetected})
	if err != nil {
		t.Fatalf("scan-all over an undetected project with a configured schema must not error, got: %v", err)
	}
	if resp.DB == nil || !resp.DB.Measured {
		t.Fatalf("the DB dimension must be measured over the configured schema, got %+v", resp.DB)
	}
	// Security is NOT MEASURED, never zero: nobody looked.
	if resp.Score.ByDimension[findings.DimensionSecurity] != nil {
		t.Errorf("security must be nil (not measured) for the sentinel language, got %+v",
			resp.Score.ByDimension[findings.DimensionSecurity])
	}
}

// TestHandleScanAll_UndetectedWithoutSchema_SaysSoHonestly is the OTHER half of
// the same declaration, and the one it would be tempting to leave unstated: the
// config init writes for such a project carries no database section, so
// scan-all has nothing to measure at all.
//
// The test asserts the error's CONTENT, not merely that an error occurred.
// Content-blindness is what let init's refusal message drift for three releases
// while naming four files that could not help; an assertion on err != nil here
// would repeat that mistake in the very change that fixes it.
func TestHandleScanAll_UndetectedWithoutSchema_SaysSoHonestly(t *testing.T) {
	root := writeProj(t, map[string]string{".codefit.yaml": undetectedYAMLNoSchema})

	_, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: config.LanguageUndetected})
	if err == nil {
		t.Fatal("scan-all with no provider and no schema must say it has nothing to audit, got nil")
	}
	msg := err.Error()
	for _, want := range []string{
		"nothing to audit",
		config.LanguageUndetected,
		"database.schema_paths",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("the error must name %q so the agent can act on it, got: %s", want, msg)
		}
	}
	// It must never read as a clean project.
	for _, forbidden := range []string{"no issues", "clean"} {
		if strings.Contains(strings.ToLower(msg), forbidden) {
			t.Errorf("the error must not read as a pass; it contains %q: %s", forbidden, msg)
		}
	}
}
