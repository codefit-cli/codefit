package security_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
	ssec "github.com/codefit-cli/codefit/internal/sensors/security"
)

// The security sensor reads source files with the same raw-bytes call the DB
// sensor used, and it was blind in the same way — MEASURED, not assumed, before
// this test was written:
//
//	utf-8            → 1 finding, score 90
//	utf-8 + BOM      → 1 finding, score 90
//	utf-16le + BOM   → 0 findings, score 100
//	utf-16le, no BOM → 0 findings, score 100
//
// A UTF-16 file is an ordinary thing to find in a repository that has ever been
// touched by a Windows tool, and score 100 over one is the same false all-clear.
//
// SCOPE, declared rather than implied: this file locks the DECODING half only.
// The source-level floor that makes the silence impossible (sensors/db's
// unread.go) is NOT replicated here, and deliberately: schema sources are a
// small CONFIGURED list where "this file reached no rule" is a fact about the
// developer's own configuration, whereas the security sensor walks a whole
// repository where a file yielding no finding is the ordinary case and a note
// per such file would be pure noise. A BOM-LESS UTF-16 source file therefore
// remains silently unread for the security dimension — a residual recorded in
// ADR 0044, not a case this test claims to cover.

const vulnerableTS = `import { db } from "./db";
const API_KEY = "sk-live-abcdefghijklmnopqrstuvwxyz0123456789ABCD";
export async function handler(req: any, res: any) {
  const rows = await db.$queryRawUnsafe("SELECT * FROM users WHERE id = " + req.params.id);
  res.json(rows);
}
`

func utf16leBytes(s string, bom bool) []byte {
	var out []byte
	if bom {
		out = append(out, 0xFF, 0xFE)
	}
	for _, r := range s {
		out = append(out, byte(r), 0x00)
	}
	return out
}

func scanOneFile(t *testing.T, name string, data []byte) (findingIDs []string, score int) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
	yaml := "project:\n  name: enc\n  language: typescript\n"
	if err := os.WriteFile(filepath.Join(root, ".codefit.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(filepath.Join(root, ".codefit.yaml"))
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	res, err := ssec.New(typescript.New()).Run(auditctx.AuditContext{ProjectRoot: root, Config: cfg, Language: "typescript"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, f := range res.Findings {
		findingIDs = append(findingIDs, f.ID)
	}
	return findingIDs, res.Score
}

// The twin lock, one level up from the DB sensor's: the SAME source in a
// BOM-marked encoding must produce the SAME findings as its UTF-8 original.
// Compared against the UTF-8 run rather than against literal numbers, so the
// test stays a statement about encoding even if the rule set moves.
func TestSensorSecurity_BOMMarkedEncodings_FindWhatUTF8Finds(t *testing.T) {
	wantIDs, wantScore := scanOneFile(t, "handler.ts", []byte(vulnerableTS))
	if len(wantIDs) == 0 {
		t.Fatalf("the UTF-8 reference run found nothing — the fixture no longer exercises any rule, so every comparison below would pass vacuously")
	}

	for name, data := range map[string][]byte{
		"utf-8 with BOM":    append([]byte{0xEF, 0xBB, 0xBF}, []byte(vulnerableTS)...),
		"utf-16le with BOM": utf16leBytes(vulnerableTS, true),
	} {
		t.Run(name, func(t *testing.T) {
			gotIDs, gotScore := scanOneFile(t, "handler.ts", data)
			if len(gotIDs) != len(wantIDs) || gotScore != wantScore {
				t.Fatalf("findings = %v (score %d), want the UTF-8 twin's %v (score %d)",
					gotIDs, gotScore, wantIDs, wantScore)
			}
		})
	}
}
