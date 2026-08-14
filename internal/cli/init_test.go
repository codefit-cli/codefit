package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/providers/registry"
	"github.com/codefit-cli/codefit/internal/scaffold"
)

func TestInitCmdHasFlags(t *testing.T) {
	cmd := newInitCmd()
	for _, name := range []string{"force", "non-interactive"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("init is missing flag --%s", name)
		}
	}
}

// TestFormatReportCreatedWithAgents pins that the report spells every path it
// prints with forward slashes, whatever the host separator is.
//
// The scaffold hands formatReport NATIVE paths — findPrismaSchema returns
// filepath.FromSlash(...) and the skill writers join with the OS separator — so
// the filepath.FromSlash calls below are what the product really passes on
// Windows, not a contrivance. Slash is codefit's canonical spelling for a path
// it emits: RenderConfig slash-normalizes the very same schema path into the
// .codefit.yaml written in the same run, so a report saying
// "prisma\schema.prisma" would contradict the config it just generated.
func TestFormatReportCreatedWithAgents(t *testing.T) {
	res := scaffold.Result{
		Info: scaffold.ProjectInfo{
			Name: "bitacora", Language: "typescript", Framework: "next",
			ORM: "prisma", DBType: "postgresql",
			SchemaPaths:   []string{filepath.FromSlash("prisma/schema.prisma")},
			RouteHandlers: 34,
		},
		ConfigPath:   ".codefit.yaml",
		ConfigAction: scaffold.ConfigCreated,
		Skills: []scaffold.SkillWrite{
			{Agent: "OpenCode", Path: filepath.FromSlash(".opencode/skills/codefit/SKILL.md")},
			{Agent: "Claude Code", Path: filepath.FromSlash(".claude/skills/codefit/SKILL.md")},
		},
	}
	out := formatReport(res)

	for _, want := range []string{
		"typescript", "next", "prisma", "postgresql", "34",
		".codefit.yaml", "OpenCode", "Claude Code",
		"prisma/schema.prisma",
		".opencode/skills/codefit/SKILL.md", ".claude/skills/codefit/SKILL.md",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q\n---\n%s", want, out)
		}
	}
	// No path the report prints may carry a host separator. Asserting the
	// absence too keeps the check from passing on a report that happens to
	// contain both spellings.
	if strings.Contains(out, `\`) {
		t.Errorf("report must not print backslash-separated paths\n---\n%s", out)
	}
}

func TestFormatReportSkippedAndFallback(t *testing.T) {
	skipped := formatReport(scaffold.Result{
		Info:         scaffold.ProjectInfo{Name: "x", Language: "go"},
		ConfigPath:   ".codefit.yaml",
		ConfigAction: scaffold.ConfigSkipped,
		UsedFallback: true,
		Skills:       []scaffold.SkillWrite{{Agent: "standard location", Path: filepath.FromSlash(".agents/skills/codefit/SKILL.md")}},
	})
	if !strings.Contains(skipped, "--force") {
		t.Errorf("skipped report must mention --force to regenerate:\n%s", skipped)
	}
	if !strings.Contains(strings.ToLower(skipped), "no known agents") {
		t.Errorf("fallback report must say no known agents were detected:\n%s", skipped)
	}
}

// End-to-end: init on a real project tree (non-interactive) writes the config and
// the skill, and reports what it did.
func TestInitCommandEndToEnd(t *testing.T) {
	root := t.TempDir()
	writeTSProject(t, root)
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	out, err := runInit(t, root, "", "--non-interactive")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	if !fileThere(filepath.Join(root, ".codefit.yaml")) {
		t.Errorf(".codefit.yaml not created")
	}
	if !fileThere(filepath.Join(root, ".claude/skills/codefit/SKILL.md")) {
		t.Errorf("skill not placed for Claude Code")
	}
	if !strings.Contains(out, "typescript") {
		t.Errorf("output should report the detected language:\n%s", out)
	}
}

// runInit executes the root command with `init <root> <extra...>` and returns its
// combined output. stdin is wired to the given reader for the interactive prompt.
func runInit(t *testing.T, root string, stdin string, extra ...string) (string, error) {
	t.Helper()
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs(append([]string{"init", root}, extra...))
	err := cmd.Execute()
	return buf.String(), err
}

func TestConfirmOverwrite(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"y\n", true},
		{"yes\n", true},
		{"Y\n", true},
		{"\n", false},  // empty → default NO
		{"n\n", false}, // explicit no
		{"", false},    // EOF → default NO
		{"nope\n", false},
	}
	for _, c := range cases {
		got, err := confirmOverwrite(&bytes.Buffer{}, strings.NewReader(c.in))
		if err != nil {
			t.Errorf("confirmOverwrite(%q): unexpected error %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("confirmOverwrite(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// The interactive flow: an existing config is regenerated on "y" and left alone
// on an empty answer — the dev always decides.
func TestInitInteractiveOverwriteDecision(t *testing.T) {
	setup := func(t *testing.T) string {
		root := t.TempDir()
		writeTSProject(t, root)
		if err := os.WriteFile(filepath.Join(root, ".codefit.yaml"), []byte("version: \"old\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return root
	}

	t.Run("yes regenerates", func(t *testing.T) {
		root := setup(t)
		out, err := runInit(t, root, "y\n")
		if err != nil {
			t.Fatalf("init: %v\n%s", err, out)
		}
		data, _ := os.ReadFile(filepath.Join(root, ".codefit.yaml"))
		if strings.Contains(string(data), `"old"`) {
			t.Errorf("config was not regenerated on 'y':\n%s", data)
		}
	})

	t.Run("empty keeps existing", func(t *testing.T) {
		root := setup(t)
		out, err := runInit(t, root, "\n")
		if err != nil {
			t.Fatalf("init: %v\n%s", err, out)
		}
		data, _ := os.ReadFile(filepath.Join(root, ".codefit.yaml"))
		if !strings.Contains(string(data), `"old"`) {
			t.Errorf("existing config was modified despite empty answer:\n%s", data)
		}
		if !strings.Contains(out, "left unchanged") {
			t.Errorf("output should report the config was left unchanged:\n%s", out)
		}
	})
}

// TestInitCommandStatesRealGoCapability is R4
// (docs/specs/declared-partial-language-exposure.md): `codefit init` on a Go
// project must state the real capability — exposed for security scanning
// (roadmap P4-1), but with its ACTUAL narrow reach named (6 declared
// security rules, 1-of-4 surface categories), DERIVED from the registry,
// never a hardcoded string and never a bare "can audit" claim that would
// read as parity with TypeScript.
func TestInitCommandStatesRealGoCapability(t *testing.T) {
	root := t.TempDir()
	writeGoProject(t, root)

	out, err := runInit(t, root, "", "--non-interactive")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	low := strings.ToLower(out)
	if !strings.Contains(low, "scan-security") && !strings.Contains(low, "scan-all") {
		t.Errorf("init output for a Go project must state security scanning is available, got:\n%s", out)
	}
	if !strings.Contains(low, "1 of 4") {
		t.Errorf("init output for a Go project must name its 1-of-4 surface category reach, got:\n%s", out)
	}
}

// TestFormatReport_UndetectedDeclaresGap is the report half of the three-place
// declaration (generated config, init report, README).
//
// The report is what the developer actually reads. It must name the markers
// codefit looked for — derived, so it cannot repeat the deleted message's
// defect of listing manifests that resolve nothing — and it must state the
// consequence rather than leaving init looking like a success.
func TestFormatReport_UndetectedDeclaresGap(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pom.xml"), []byte("<project/>\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := scaffold.Generate(scaffold.Options{Root: root})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	out := formatReport(res)
	low := strings.ToLower(out)

	markers := registry.InitDetectMarkerFiles()
	if len(markers) == 0 {
		t.Fatal("no InitDetect markers registered; the loop below would pass vacuously")
	}
	for _, m := range markers {
		if !strings.Contains(out, m) {
			t.Errorf("the report must name the marker %q codefit looks for\n---\n%s", m, out)
		}
	}
	// The counter-probe carries the SAME four manifests its two siblings do
	// (scaffold's TestRenderConfig_UndetectedNamesTheMarkersItLooksFor and
	// TestCapabilityStatement_Undetected). pom.xml is not optional here just
	// because it is the fixture's own file: the report never echoes the fixture,
	// and omitting it left a hole — under a mutation that made
	// InitDetectMarkerFiles also return pom.xml, this test stayed GREEN while
	// both siblings went RED. The three declaration sites must move together, or
	// the one a developer actually reads is the one left unguarded.
	for _, cannotHelp := range []string{"pyproject.toml", "requirements.txt", "pom.xml", "build.gradle"} {
		if strings.Contains(out, cannotHelp) {
			t.Errorf("the report names %q, which resolves no provider — the original defect\n---\n%s", cannotHelp, out)
		}
	}
	if !strings.Contains(out, "schema_paths") {
		t.Errorf("the report must say the DB dimension still audits a configured schema\n---\n%s", out)
	}
	if !strings.Contains(low, "migration") {
		t.Errorf("the report must declare that SQL migration directories are NOT detected\n---\n%s", out)
	}
	if !strings.Contains(low, "scan-all") {
		t.Errorf("the report must name the consequence for the next codefit-scan-all\n---\n%s", out)
	}
}

// reportFor renders the report for an info, with the surrounding Result fields
// held constant so the only thing varying between cases is the DB detection.
func reportFor(info scaffold.ProjectInfo) string {
	return formatReport(scaffold.Result{
		Info:         info,
		ConfigPath:   ".codefit.yaml",
		ConfigAction: scaffold.ConfigCreated,
		UsedFallback: true,
		Skills:       []scaffold.SkillWrite{{Agent: "standard location", Path: ".agents/skills/codefit/SKILL.md"}},
	})
}

// TestFormatReport_SchemaGapDeclaredWheneverNoSchemaPaths is R2's report half,
// and it REPLACES TestFormatReport_SchemaGapDeclaredWheneverNoORM rather than
// sitting beside it.
//
// The old test could not tell the two predicates apart. Its counter-case set ORM
// *and* SchemaPaths at once, so `info.ORM != ""` and `len(info.SchemaPaths) > 0`
// were true together in every case it had; the whole predicate move was invisible
// to it. Retargeting the counter-case — not adding a third one next to it — is
// what makes the suite able to see the change at all.
//
// The gap declaration follows the field the audit actually reads: schema_paths is
// the only DB field any sensor consumes, so it is the only one that can decide
// whether this config audits a schema.
func TestFormatReport_SchemaGapDeclaredWheneverNoSchemaPaths(t *testing.T) {
	declared := map[string]scaffold.ProjectInfo{
		"detected, no orm, no schema": {Name: "x", Language: "typescript", Framework: "next"},
		// The drizzle/typeorm shape, reachable today through real detection: an
		// ORM is named, no schema source is. Under the ORM-keyed predicate this
		// was the one case that got NO declaration while having the full gap.
		"orm detected but no schema": {Name: "x", Language: "typescript", ORM: "drizzle"},
	}
	for name, info := range declared {
		t.Run(name, func(t *testing.T) {
			out := reportFor(info)
			if !strings.Contains(out, schemaGapHeading) {
				t.Errorf("this config audits no schema, so the report must declare it\n---\n%s", out)
			}
		})
	}

	// The counter-cases: a config that DOES name a schema source must not be
	// handed a warning that does not apply to it.
	notDeclared := map[string]scaffold.ProjectInfo{
		"orm and schema": {
			Name: "x", Language: "typescript", ORM: "prisma", DBType: "postgresql",
			SchemaPaths: []string{filepath.FromSlash("prisma/schema.prisma")},
		},
		// HAND-BUILT ON PURPOSE, and it is the discriminating case. No detection
		// path produces SchemaPaths without an ORM today — that is precisely the
		// shape this change exists to stop mis-handling, and the shape SQL
		// migration detection will make real. This project prefers fixtures driven
		// through the real parser; here there is no real path to drive, and saying
		// so is better than pretending the case is unreachable and leaving the
		// predicate unlocked.
		//
		// It also cannot pass on the old predicate BY CONSTRUCTION: writeSchemaGap
		// was `if info.ORM != "" { return }`, so with ORM == "" the old code had no
		// path at all that skipped the section.
		"schema without orm": {
			Name: "x", Language: "typescript",
			SchemaPaths: []string{filepath.FromSlash("db/migrations")},
		},
	}
	for name, info := range notDeclared {
		t.Run(name, func(t *testing.T) {
			out := reportFor(info)
			if strings.Contains(out, schemaGapHeading) {
				t.Errorf("this config names a schema source, so it must not be told none was "+
					"detected\n---\n%s", out)
			}
		})
	}
}

// schemaGapHeading anchors the section writeSchemaGap emits. Anchoring on the
// heading rather than on a loose word keeps "the section is present" from being
// satisfied by any other sentence that happens to mention a schema.
const schemaGapHeading = "Not configured — database schema:"

// liveYAMLKeyPresent reports whether raw carries key as a LIVE (non-comment) YAML
// key. The generated config DISCUSSES `database:` and `schema_paths` in prose for
// every project that has neither, so a plain substring search would answer "yes"
// for exactly the case this asks about.
func liveYAMLKeyPresent(raw, key string) bool {
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, key+":") {
			return true
		}
	}
	return false
}

// TestInit_DrizzleProjectOmitsDatabaseBlockAndDeclaresGap is R1's live case, and
// it is deliberately driven through the REAL init command over a REAL project
// tree rather than a hand-built ProjectInfo — this shape is reachable today, by
// one of the most common TypeScript ORMs, and the defect it pins was found by
// running the binary.
//
// Drizzle sets ORM and never SchemaPaths (detect.go's enrichTypeScript). Under
// the old, ORM-keyed gate that produced the worst of both artifacts at once: a
// `database:` block containing nothing but `orm: drizzle` — a section that
// configures nothing any sensor reads — AND no schema-gap declaration at all,
// because writeSchemaGap returned early on a non-empty ORM. A header that looks
// configured, suppressing the declaration that says it is not.
//
// Both halves are asserted together because fixing either alone leaves the
// contradiction standing.
func TestInit_DrizzleProjectOmitsDatabaseBlockAndDeclaresGap(t *testing.T) {
	root := t.TempDir()
	writeDrizzleProject(t, root)

	out, err := runInit(t, root, "", "--non-interactive")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}

	raw, err := os.ReadFile(filepath.Join(root, ".codefit.yaml"))
	if err != nil {
		t.Fatalf("reading the config init wrote: %v", err)
	}
	cfgText := string(raw)

	// The fixture is verified by its EFFECT, not by its name: if detection did
	// not actually see drizzle, everything below would pass vacuously.
	if !strings.Contains(out, "drizzle") {
		t.Fatalf("the fixture did not make init detect drizzle, so this test proves nothing about "+
			"the drizzle shape\n---\n%s", out)
	}

	if liveYAMLKeyPresent(cfgText, "database") {
		t.Errorf("a drizzle project has no schema source codefit can read, so the config must carry "+
			"NO live `database:` block. An orm-only block configures nothing any sensor reads while "+
			"looking configured\n--- config ---\n%s", cfgText)
	}
	if !strings.Contains(out, schemaGapHeading) {
		t.Errorf("the report must declare the schema gap for a drizzle project: the config it just "+
			"wrote audits no schema\n--- report ---\n%s", out)
	}
	// The detection FACT survives the block's removal — the developer meets the
	// consequence, they are not quietly deprived of what codefit saw.
	if !strings.Contains(out, "orm") {
		t.Errorf("the report must still state that an ORM was detected, or removing the block hides "+
			"a fact instead of explaining it\n---\n%s", out)
	}
}

// TestInitCommandOnUnregisteredStackExits0 is the end-to-end replacement for
// the behaviour this change removed: the command used to exit non-zero and
// write nothing.
func TestInitCommandOnUnregisteredStackExits0(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "pom.xml"), []byte("<project/>\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runInit(t, root, "", "--non-interactive")
	if err != nil {
		t.Fatalf("init on an unregistered stack must succeed, got: %v\n%s", err, out)
	}
	if !fileThere(filepath.Join(root, ".codefit.yaml")) {
		t.Errorf(".codefit.yaml not created:\n%s", out)
	}
	if !fileThere(filepath.Join(root, ".agents/skills/codefit/SKILL.md")) {
		t.Errorf("skill not placed:\n%s", out)
	}
	if strings.Contains(out, "not a registered language") {
		t.Errorf("init rendered the unregistered-language fallback:\n%s", out)
	}
}

func writeGoProject(t *testing.T, root string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module demo\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeDrizzleProject writes a TypeScript project whose package.json declares
// drizzle-orm and which has NO prisma/schema.prisma — the shape enrichTypeScript
// resolves to ORM="drizzle" with SchemaPaths left empty.
func writeDrizzleProject(t *testing.T, root string) {
	t.Helper()
	writeProjectFile(t, root, "package.json",
		`{"name":"demo","dependencies":{"next":"14.0.0","drizzle-orm":"0.30.0"}}`)
	writeProjectFile(t, root, "tsconfig.json", `{"compilerOptions":{"strict":true}}`)
	writeProjectFile(t, root, "app/users/route.ts",
		"export async function GET() { return Response.json([]); }\n")
}

func writeProjectFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeTSProject(t *testing.T, root string) {
	t.Helper()
	write := func(rel, content string) {
		full := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", `{"name":"demo","dependencies":{"next":"14.0.0","react":"18.0.0"}}`)
	write("tsconfig.json", `{"compilerOptions":{"strict":true}}`)
	write("app/users/route.ts", "export async function GET() { return Response.json(await prisma.user.findMany()); }\n")
}

func fileThere(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
