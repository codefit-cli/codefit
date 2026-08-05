package mcp_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/mcp"
)

const regressionTSYAMLNoDB = `version: "1"
project:
  name: t
  language: typescript
  framework: next
`

const regressionTSYAMLWithDB = `version: "1"
project:
  name: t
  language: typescript
  framework: next
database:
  type: postgresql
  schema_paths:
    - prisma/schema.prisma
`

const regressionNoPKSchema = `datasource db {
  provider = "postgresql"
  url      = env("DATABASE_URL")
}

model NoKey {
  name String
}
`

// stripKey unmarshals raw JSON into a generic map, deletes key, and
// re-marshals — used to compare "everything except the new field" between
// two JSON payloads with a stable (alphabetical, from encoding/json's map
// marshaling) key order on both sides.
func stripKey(t *testing.T, raw []byte, key string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	delete(m, key)
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(out)
}

// TestScanAll_TypeScriptHappyPath_OnlyDiffIsAddedSecurityKey is the modified
// requirement's exact success criterion, made checkable rather than asserted:
// "every pre-existing field value and the baseline delta identical", NOT
// "byte-identical" — the response gains exactly one field (`security`) and
// nothing else about it changes, for a TypeScript project with a resolvable
// provider, with and without a configured schema.
//
// The golden is the REAL pre-change response (commit 337f158, before this
// change), captured via `git worktree add --detach` and dumped with
// json.MarshalIndent — not a re-implementation of what the old shape "should"
// have produced (see testdata/README.md).
//
// Mutation gate (design's modified-requirement note): any unconditional
// addition to the response for a resolvable language — e.g. always setting a
// note on Security even when secRan is true, or letting `scanned`'s
// construction reorder observedFrom's dedup — flips this test from pass to
// fail.
func TestScanAll_TypeScriptHappyPath_OnlyDiffIsAddedSecurityKey(t *testing.T) {
	cases := []struct {
		name   string
		golden string
		files  map[string]string
	}{
		{
			name:   "no-db",
			golden: "scanall_ts_nodb_prechange.json",
			files: map[string]string{
				".codefit.yaml":  regressionTSYAMLNoDB,
				"app/x/route.ts": "export async function GET() { return Response.json({}); }\n",
			},
		},
		{
			name:   "with-db",
			golden: "scanall_ts_withdb_prechange.json",
			files: map[string]string{
				".codefit.yaml":        regressionTSYAMLWithDB,
				"prisma/schema.prisma": regressionNoPKSchema,
				"app/x/route.ts":       "export async function GET() { return Response.json({}); }\n",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := writeProj(t, tc.files)
			resp, err := mcp.HandleScanAll(mcp.ScanAllRequest{Root: root, Language: "typescript"})
			if err != nil {
				t.Fatalf("HandleScanAll: %v", err)
			}

			// The new field is present and reports security as measured (no
			// caveat) — this IS the added key, asserted before it is stripped.
			if !resp.Security.Measured || resp.Security.Note != "" {
				t.Fatalf("a TypeScript project (provider resolves) must have security.measured=true and no note, got %+v",
					resp.Security)
			}

			golden, err := os.ReadFile(filepath.Join("testdata", tc.golden))
			if err != nil {
				t.Fatalf("reading pre-change golden %s: %v", tc.golden, err)
			}
			live, err := json.Marshal(resp)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}

			gotWithoutSecurity := stripKey(t, live, "security")
			wantWithoutSecurity := stripKey(t, golden, "security") // golden has no "security" key; delete is a no-op, kept for symmetry
			if gotWithoutSecurity != wantWithoutSecurity {
				t.Errorf("the change moved a pre-existing field for a resolvable-language project.\n"+
					"pre-change (minus security): %s\npost-change (minus security): %s", wantWithoutSecurity, gotWithoutSecurity)
			}
		})
	}
}
