package security_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/golang"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
	"github.com/codefit-cli/codefit/internal/sensors/security"
)

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runSensor(t *testing.T, root string) findings.SensorResult {
	t.Helper()
	cfg := &config.Config{Project: config.Project{
		Language: "go",
		PathCriticality: config.PathCriticality{
			Production: []string{"**/*.go"},
			Test:       []string{"**/*_test.go"},
			Example:    []string{"examples/**"},
		},
	}}
	ctx := auditctx.AuditContext{ProjectRoot: root, Language: "go", Config: cfg}
	res, err := security.New(golang.New()).Run(ctx)
	if err != nil {
		t.Fatalf("sensor Run: %v", err)
	}
	return res
}

func find(fs []findings.Finding, fileSuffix, id string) *findings.Finding {
	for i := range fs {
		if fs[i].ID == id && strings.HasSuffix(fs[i].File, fileSuffix) {
			return &fs[i]
		}
	}
	return nil
}

// runSensorProvider runs the sensor with an explicit provider/language, so the
// same scenario can be exercised across languages (the sensor is provider-
// agnostic; the dedup must hold for every provider, not just Go).
func runSensorProvider(t *testing.T, root, lang, prodGlob string, p providers.LanguageProvider) findings.SensorResult {
	t.Helper()
	cfg := &config.Config{Project: config.Project{
		Language: lang,
		PathCriticality: config.PathCriticality{
			Production: []string{prodGlob},
			Test:       []string{"**/*_test." + lang},
			Example:    []string{"examples/**"},
		},
	}}
	ctx := auditctx.AuditContext{ProjectRoot: root, Language: lang, Config: cfg}
	res, err := security.New(p).Run(ctx)
	if err != nil {
		t.Fatalf("sensor Run (%s): %v", lang, err)
	}
	return res
}

// countFindings counts findings with the given id in the given file — the
// metric the latent Go duplication never measured (the old tests only asserted
// existence via find(), so a double-report passed unnoticed).
func countFindings(fs []findings.Finding, fileSuffix, id string) int {
	n := 0
	for i := range fs {
		if fs[i].ID == id && strings.HasSuffix(fs[i].File, fileSuffix) {
			n++
		}
	}
	return n
}

// TestSensorDedupesOverlappingSecretFindings forces the layer-1 (shape) and
// layer-2 (name) secret detectors to BOTH fire on the same line — a value with
// an API-key shape assigned to a credential-named variable — and asserts the
// sensor reports it exactly ONCE, keeping the higher (critical) severity.
//
// The fixture is the one the historical Go test dodged: the old fixture used
// "super-secret-value-123", which never matched a layer-1 pattern, so the
// duplication stayed latent. Here the value is sk-ant-shaped, so without dedup
// both layers fire and the count is 2 (red). This runs for BOTH languages,
// because the overlap (and the fix) live in the provider-agnostic sensor.
func TestSensorDedupesOverlappingSecretFindings(t *testing.T) {
	const apiKeyValue = `"sk-ant-api03-AbCdEf1234567890"`

	cases := []struct {
		lang, prodGlob, file, src string
		provider                  providers.LanguageProvider
	}{
		{
			lang: "go", prodGlob: "**/*.go", file: "secret.go",
			src:      "package x\nvar apiKey = " + apiKeyValue + "\n",
			provider: golang.New(),
		},
		{
			lang: "ts", prodGlob: "**/*.ts", file: "secret.ts",
			src:      "const apiKey = " + apiKeyValue + ";\n",
			provider: typescript.New(),
		},
	}
	for _, c := range cases {
		t.Run(c.lang, func(t *testing.T) {
			root := t.TempDir()
			writeFile(t, root, c.file, c.src)
			res := runSensorProvider(t, root, c.lang, c.prodGlob, c.provider)

			if n := countFindings(res.Findings, c.file, "SEC-001"); n != 1 {
				t.Fatalf("want exactly 1 SEC-001 (layer-1 and layer-2 collapsed), got %d", n)
			}
			// The surviving finding keeps the higher severity: layer 1's critical.
			if f := find(res.Findings, c.file, "SEC-001"); f == nil || f.Severity != findings.SeverityCritical {
				t.Errorf("surviving SEC-001 should keep the critical severity, got %+v", f)
			}
		})
	}
}

// TestSensorTypeScriptEndToEnd is the slice-8 close-out: the provider-agnostic
// sensor, driven by the real TypeScript provider, produces a complete
// SensorResult over a multi-file TS project — findings from all five security
// categories, a computed security score, no findings on clean code, and an
// empty surface (surface mapping is Fase 1.3, still a stub).
func TestSensorTypeScriptEndToEnd(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "src/secrets.ts", `const password = "hunter2hunter2";`)                                                   // SEC-001
	writeFile(t, root, "src/crypto.ts", `export const h = md5(data);`)                                                           // SEC-052
	writeFile(t, root, "src/eval.ts", `export function run(i: string) { eval(i); }`)                                             // SEC-014
	writeFile(t, root, "src/sql.ts", `export function q(db: any, id: string) { db.query("SELECT * FROM t WHERE id = " + id); }`) // SEC-010
	writeFile(t, root, "src/view.tsx", `export const V = (h: string) => <div dangerouslySetInnerHTML={{__html: h + "!"}}/>;`)    // SEC-079
	writeFile(t, root, "src/clean.ts", `export function add(a: number, b: number) { return a + b; }`)                            // none

	res := runSensorProvider(t, root, "ts", "**/*", typescript.New())

	for _, id := range []string{"SEC-001", "SEC-052", "SEC-014", "SEC-010", "SEC-079"} {
		if find(res.Findings, ".ts", id) == nil && find(res.Findings, ".tsx", id) == nil {
			t.Errorf("expected a %s finding somewhere in the TS project", id)
		}
	}
	// Clean file is silent.
	for i := range res.Findings {
		if strings.HasSuffix(res.Findings[i].File, "clean.ts") {
			t.Errorf("clean.ts must produce no findings, got %+v", res.Findings[i])
		}
	}
	// Security dimension is scored, and findings drag it below a perfect 100.
	if res.Score < 0 || res.Score >= 100 {
		t.Errorf("security score should be computed and below 100 with findings, got %d", res.Score)
	}
	// None of these files is a Next route handler, so there is no IDOR surface.
	if len(res.Surface) != 0 {
		t.Errorf("non-route TS files carry no surface, got %d items", len(res.Surface))
	}
}

// TestSensorMapsIDORSurface is the surface close-out: the real sensor walk over
// a Next App Router project produces IDOR surface in the SensorResult, with the
// stable id stamped (so the agent can confirm it) and signals that are facts.
func TestSensorMapsIDORSurface(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "app/users/[id]/route.ts", `
export async function GET(req: Request, { params }: { params: { id: string } }) {
  return Response.json(await prisma.user.findUnique({ where: { id: params.id } }));
}`)
	writeFile(t, root, "app/health/route.ts", `export async function GET() { return Response.json({ ok: true }); }`)
	writeFile(t, root, "lib/util.ts", `export const add = (a: number, b: number) => a + b;`)

	res := runSensorProvider(t, root, "ts", "**/*", typescript.New())

	var idor []findings.SurfaceItem
	for _, it := range res.Surface {
		if it.Category == "idor" {
			idor = append(idor, it)
		}
	}
	if len(idor) != 1 {
		t.Fatalf("want exactly 1 IDOR surface item (only the [id] route), got %d: %+v", len(idor), res.Surface)
	}
	it := idor[0]
	if it.ID == "" {
		t.Error("surface item flowing through the sensor must carry a stable id for confirmation")
	}
	if len(it.StructuralSignals) != 3 || !strings.HasSuffix(strings.TrimSpace(it.ReasonToReview), "?") {
		t.Errorf("IDOR item must carry 3 fact signals and a question, got %+v", it)
	}
}

// TestSensorTypeScriptCriticalityApplies confirms path_criticality adjusts TS
// findings exactly as it does for Go — the adjustment lives in the agnostic
// sensor, so a credential in an example file is forced down to info.
func TestSensorTypeScriptCriticalityApplies(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "examples/demo.ts", `const apiKey = "sk-ant-api03-AbCdEf1234567890";`)
	res := runSensorProvider(t, root, "ts", "src/**", typescript.New())

	f := find(res.Findings, "demo.ts", "SEC-001")
	if f == nil {
		t.Fatal("expected a SEC-001 finding in the example TS file")
	}
	if f.Severity != findings.SeverityInfo {
		t.Errorf("a finding in an example file must be forced to info, got %q", f.Severity)
	}
}

func TestSensorIdentity(t *testing.T) {
	s := security.New(golang.New())
	if s.Name() != "security" || s.Dimension() != findings.DimensionSecurity {
		t.Errorf("identity = %q/%q", s.Name(), s.Dimension())
	}
}

func TestSensorFindsASTSecurityIssues(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "auth.go", `package x
import "crypto/md5"
func f() {
	apiKey := "super-secret-value-1234"
	_ = apiKey
	_ = md5.New()
}`)
	res := runSensor(t, root)

	if f := find(res.Findings, "auth.go", "SEC-001"); f == nil || f.Severity != findings.SeverityHigh {
		t.Errorf("want SEC-001 high in auth.go, got %+v", f)
	}
	if f := find(res.Findings, "auth.go", "SEC-052"); f == nil || f.Severity != findings.SeverityMedium {
		t.Errorf("want SEC-052 medium in auth.go, got %+v", f)
	}
}

func TestSensorLayer1DetectsAPIKeyPattern(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "config.go", `package x
var k = "sk-ant-api03-AbCdEf1234567890"
`)
	res := runSensor(t, root)
	f := find(res.Findings, "config.go", "SEC-001")
	if f == nil || f.Severity != findings.SeverityCritical {
		t.Errorf("want a critical SEC-001 from the API-key pattern, got %+v", f)
	}
}

func TestSensorDowngradesInTestFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "config_test.go", `package x
var k = "sk-ant-api03-AbCdEf1234567890"
`)
	res := runSensor(t, root)
	f := find(res.Findings, "config_test.go", "SEC-001")
	if f == nil {
		t.Fatal("expected a finding in the test file")
	}
	if f.Severity == findings.SeverityCritical {
		t.Error("critical finding in a test file should be downgraded")
	}
	if f.Severity != findings.SeverityHigh {
		t.Errorf("critical should downgrade to high in tests, got %q", f.Severity)
	}
}

func TestSensorMapsSurface(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "routes.go", `package x
import "net/http"
func ListPlants(w http.ResponseWriter, r *http.Request) {}
`)
	res := runSensor(t, root)
	if len(res.Surface) == 0 {
		t.Fatal("sensor should map the HTTP handler as surface")
	}
	found := false
	for _, it := range res.Surface {
		if it.Category == "authz" && strings.HasSuffix(it.File, "routes.go") {
			found = true
		}
	}
	if !found {
		t.Errorf("want an authz surface item for routes.go, got %+v", res.Surface)
	}
}

func TestSensorExampleFilesAreInfo(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "examples/demo.go", `package x
var k = "sk-ant-api03-AbCdEf1234567890"
`)
	res := runSensor(t, root)
	f := find(res.Findings, "demo.go", "SEC-001")
	if f == nil || f.Severity != findings.SeverityInfo {
		t.Errorf("findings in examples should be info, got %+v", f)
	}
}

// TestOwnedCategories_IncludesNPlus1: the unified baseline (ADR 0019) can only
// track/prune an N+1 surface item if the security sensor declares
// surface.CategoryNPlus1 in its owned scope, the same as idor/authz/overfetch.
func TestOwnedCategories_IncludesNPlus1(t *testing.T) {
	owned := security.New(nil).OwnedCategories()
	found := false
	for _, c := range owned {
		if c == string(surface.CategoryNPlus1) {
			found = true
		}
	}
	if !found {
		t.Errorf("OwnedCategories() = %v, want it to include %q (surface.CategoryNPlus1)", owned, surface.CategoryNPlus1)
	}
}
