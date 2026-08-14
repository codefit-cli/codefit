package scaffold_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	"github.com/codefit-cli/codefit/internal/providers/registry"
	"github.com/codefit-cli/codefit/internal/scaffold"
)

// loadRendered renders the config for info, writes it to a temp .codefit.yaml,
// and loads it back through the real config loader+validator.
func loadRendered(t *testing.T, info scaffold.ProjectInfo) *config.Config {
	t.Helper()
	data, err := scaffold.RenderConfig(info)
	if err != nil {
		t.Fatalf("RenderConfig: %v", err)
	}
	if !strings.Contains(string(data), "#") {
		t.Errorf("generated config should be commented for humans, got:\n%s", data)
	}
	path := filepath.Join(t.TempDir(), ".codefit.yaml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("generated config does not round-trip through config.Load: %v\n--- yaml ---\n%s", err, data)
	}
	return cfg
}

func TestRenderConfigNextPrismaRoundTrips(t *testing.T) {
	info, err := scaffold.Detect(sampleNext)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	cfg := loadRendered(t, info)

	if cfg.Project.Name != "sample-next" {
		t.Errorf("name = %q, want sample-next", cfg.Project.Name)
	}
	if cfg.Project.Language != "typescript" {
		t.Errorf("language = %q, want typescript", cfg.Project.Language)
	}
	if cfg.Project.Framework != "next" {
		t.Errorf("framework = %q, want next", cfg.Project.Framework)
	}
	if cfg.Database.ORM != "prisma" {
		t.Errorf("orm = %q, want prisma", cfg.Database.ORM)
	}
	if cfg.Database.Type != "postgresql" {
		t.Errorf("db type = %q, want postgresql", cfg.Database.Type)
	}
	if cfg.Database.Paradigm != "auto" {
		t.Errorf("paradigm = %q, want auto", cfg.Database.Paradigm)
	}
	if !slices.Contains(cfg.Database.SchemaPaths, "prisma/schema.prisma") {
		t.Errorf("schema paths = %v, want to contain prisma/schema.prisma", cfg.Database.SchemaPaths)
	}
	if !slices.Contains(cfg.Project.PathCriticality.Production, "app/**") {
		t.Errorf("production globs = %v, want app/**", cfg.Project.PathCriticality.Production)
	}
}

// A project dir name with YAML-hostile characters (quotes, backslashes) must
// still produce a config that loads, with the name preserved verbatim — not a
// silent broken file reported as success.
func TestRenderConfigEscapesHostileName(t *testing.T) {
	for _, name := range []string{`my"weird`, `back\slash`, `with: colon`, `1leading`} {
		info := scaffold.ProjectInfo{
			Name:            name,
			Language:        "go",
			PathCriticality: config.PathCriticality{Production: []string{"**/*.go"}},
		}
		cfg := loadRendered(t, info)
		if cfg.Project.Name != name {
			t.Errorf("name round-trip: got %q, want %q", cfg.Project.Name, name)
		}
	}
}

// undetectedInfo is what Detect really returns for a root holding no marker
// file that resolves a provider — built through the REAL Detect over a real
// directory rather than by hand, so the rendered config is the one a developer
// actually receives.
func undetectedInfo(t *testing.T) scaffold.ProjectInfo {
	t.Helper()
	info, err := scaffold.Detect(t.TempDir())
	if err != nil {
		t.Fatalf("Detect on an empty root: %v", err)
	}
	if info.Detected() {
		t.Fatalf("fixture is not the undetected case: language = %q", info.Language)
	}
	return info
}

// TestRenderConfig_UndetectedOmitsPathCriticalityWhole is D6. With no provider
// defaults there are no globs to emit, and the key is omitted ENTIRELY rather
// than emitted empty.
//
// The difference is not cosmetic. `path_criticality:` with nothing under it
// parses as YAML null, which reads as "configured, and empty" — a developer
// scanning the file sees a classification that was made and came out blank. The
// key's absence, plus a comment block in its place, reads as what actually
// happened: codefit never classified anything, and here is how to.
func TestRenderConfig_UndetectedOmitsPathCriticalityWhole(t *testing.T) {
	info := undetectedInfo(t)
	data, err := scaffold.RenderConfig(info)
	if err != nil {
		t.Fatalf("RenderConfig: %v", err)
	}
	out := string(data)

	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue // the comment block is where path_criticality SHOULD be discussed
		}
		if strings.HasPrefix(trimmed, "path_criticality:") || strings.HasPrefix(trimmed, "production:") {
			t.Errorf("undetected config must omit the path_criticality key whole; found live key %q\n---\n%s", trimmed, out)
		}
	}

	// The comment must state the RF-10 consequence AND how to set globs —
	// inform, do not decide.
	low := strings.ToLower(out)
	if !strings.Contains(low, "path_criticality") {
		t.Errorf("the comment block must still name path_criticality so the developer can find it\n---\n%s", out)
	}
	if !strings.Contains(low, "natural severity") {
		t.Errorf("the comment must state the RF-10 consequence: nothing is classified, so severity is not re-weighted\n---\n%s", out)
	}
	if !strings.Contains(out, "production:") {
		t.Errorf("the comment must SHOW how to set globs, not just mention that one can\n---\n%s", out)
	}
}

// TestRenderConfig_UndetectedRoundTrips: the file codefit writes must load
// through the very validator writeConfig runs on it.
func TestRenderConfig_UndetectedRoundTrips(t *testing.T) {
	cfg := loadRendered(t, undetectedInfo(t))
	if cfg.Project.Language != config.LanguageUndetected {
		t.Errorf("language = %q, want %q", cfg.Project.Language, config.LanguageUndetected)
	}
	if got := cfg.PathCriticalityFor("src/x_test.go"); got != "" {
		t.Errorf("PathCriticalityFor = %q, want \"\" — no path may be classified when codefit invented no globs", got)
	}
}

// TestRenderConfig_UndetectedNamesTheMarkersItLooksFor: the generated config is
// the artifact the developer keeps. It must say what codefit looked for, in
// names taken from the registry rather than typed by hand.
func TestRenderConfig_UndetectedNamesTheMarkersItLooksFor(t *testing.T) {
	data, err := scaffold.RenderConfig(undetectedInfo(t))
	if err != nil {
		t.Fatalf("RenderConfig: %v", err)
	}
	out := string(data)
	markers := registry.InitDetectMarkerFiles()
	if len(markers) == 0 {
		t.Fatal("no InitDetect markers registered; the loop below would pass vacuously")
	}
	for _, m := range markers {
		if !strings.Contains(out, m) {
			t.Errorf("the generated config must name the marker %q codefit looks for\n---\n%s", m, out)
		}
	}
	for _, cannotHelp := range []string{"pyproject.toml", "requirements.txt", "pom.xml", "build.gradle"} {
		if strings.Contains(out, cannotHelp) {
			t.Errorf("the generated config names %q, which resolves no provider — the original defect\n---\n%s", cannotHelp, out)
		}
	}
}

// configGapClaim anchors the sentence the generated config's else-branch makes
// about detection. Anchoring on the CLAIM rather than on a loose word keeps the
// counter-cases below from being satisfied by any other mention of a schema.
const configGapClaim = "NOT detected"

func renderOrFail(t *testing.T, info scaffold.ProjectInfo) string {
	t.Helper()
	data, err := scaffold.RenderConfig(info)
	if err != nil {
		t.Fatalf("RenderConfig: %v", err)
	}
	return string(data)
}

// TestRenderConfig_DeclaresTheSchemaGapWheneverNoSchemaPaths is R2's config half,
// and it REPLACES TestRenderConfig_DeclaresTheSchemaGapWheneverNoORM rather than
// sitting beside it.
//
// The old test's counter-case set ORM *and* SchemaPaths at once, so the two
// candidate predicates — `.ORM` and `len(.SchemaPaths) > 0` — were true together
// in every case it had. It could not see the difference between them, which means
// it locked nothing about the gate. Retargeting that counter-case is the fix; a
// third case added next to it would have left the hole exactly where it was.
//
// schema_paths is the only DB field any sensor reads, so it is the only one that
// can decide whether this config audits a schema.
func TestRenderConfig_DeclaresTheSchemaGapWheneverNoSchemaPaths(t *testing.T) {
	declared := map[string]scaffold.ProjectInfo{
		"undetected":          undetectedInfo(t),
		"detected but no orm": {Name: "x", Language: "go"},
		"typescript without orm": {Name: "x", Language: "typescript",
			PathCriticality: config.PathCriticality{Production: []string{"src/**"}}},
		// The drizzle/typeorm shape, reachable today through real detection. Under
		// the ORM-keyed gate it received a `database:` block holding nothing but
		// `orm: drizzle` — configuring nothing any sensor reads — and, because the
		// block was present, no declaration that its schema was unaudited.
		"orm detected but no schema": {Name: "x", Language: "typescript", ORM: "drizzle",
			PathCriticality: config.PathCriticality{Production: []string{"src/**"}}},
	}
	for name, info := range declared {
		t.Run(name, func(t *testing.T) {
			out := renderOrFail(t, info)
			if liveYAMLKeyPresent(out, "database") {
				t.Errorf("no schema source was detected, so no live `database:` block may be "+
					"written\n---\n%s", out)
			}
			if !strings.Contains(out, "schema_paths") {
				t.Errorf("a config auditing no schema must tell the developer schema_paths is how the "+
					"DB dimension turns on\n---\n%s", out)
			}
			if !strings.Contains(out, configGapClaim) {
				t.Errorf("the declaration must say SQL migration directories are %s — the gap that "+
					"leaves this config auditing nothing\n---\n%s", configGapClaim, out)
			}
			if !strings.Contains(strings.ToLower(out), "migration") {
				t.Errorf("the declaration must name what is not detected: SQL migration "+
					"directories\n---\n%s", out)
			}
		})
	}

	notDeclared := map[string]scaffold.ProjectInfo{
		"orm and schema": {Name: "x", Language: "typescript", ORM: "prisma",
			SchemaPaths: []string{"prisma/schema.prisma"}},
		// HAND-BUILT ON PURPOSE, and it is the discriminating case: no detection
		// path produces SchemaPaths without an ORM today. That is exactly the shape
		// this change stops mis-handling and the shape SQL migration detection will
		// make real. This project prefers fixtures driven through the real parser;
		// there is no real path to drive here, and leaving the predicate unlocked
		// would be the worse trade.
		"schema without orm": {Name: "x", Language: "typescript",
			SchemaPaths: []string{"db/migrations"}},
	}
	for name, info := range notDeclared {
		t.Run(name, func(t *testing.T) {
			out := renderOrFail(t, info)
			if !liveYAMLKeyPresent(out, "schema_paths") {
				t.Errorf("a detected schema source must be written as a LIVE schema_paths key, not "+
					"discussed in a comment\n---\n%s", out)
			}
			if strings.Contains(out, configGapClaim) {
				t.Errorf("this config names a schema source, so it must not also claim schemas are "+
					"%s\n---\n%s", configGapClaim, out)
			}
		})
	}
}

// TestRenderConfig_OrmDeclaresThatNothingReadsIt is R4. `orm:` round-trips and is
// user-visible, so it stays — but an unread field printed beside read ones invites
// the belief that setting it does something. codefit informs the consequence
// rather than deleting a committed key or leaving the impression standing.
func TestRenderConfig_OrmDeclaresThatNothingReadsIt(t *testing.T) {
	out := renderOrFail(t, scaffold.ProjectInfo{
		Name: "x", Language: "typescript", ORM: "prisma", DBType: "postgresql",
		SchemaPaths: []string{"prisma/schema.prisma"},
	})
	if !liveYAMLKeyPresent(out, "orm") {
		t.Fatalf("orm: must still be emitted for a project that has one\n---\n%s", out)
	}

	// The statement lives in a comment, so it must be found in the COMMENTS, not
	// anywhere in the file: a live key spelling these words would be a different
	// (and broken) artifact.
	comments := configComments(out)
	low := strings.ToLower(comments)
	if !strings.Contains(low, "no sensor reads") {
		t.Errorf("the config must state that no sensor reads orm:, or a developer will believe "+
			"setting it turns something on\n--- comments ---\n%s", comments)
	}
	if !strings.Contains(comments, "schema_paths") {
		t.Errorf("the same statement must name schema_paths as what actually turns the DB dimension "+
			"on\n--- comments ---\n%s", comments)
	}
}

// TestRenderConfig_CommentedExampleNamesTypeAndItsConsequence is R5.
//
// The commented example is not decoration: it is the instruction a developer with
// a SQL migration directory follows by hand, and it is the ONLY reachable route
// into sqlDialectParser today. Showing schema_paths alone steers a MySQL or SQL
// Server user straight into the "" branch, which parses their DDL as PostgreSQL
// and says nothing — a silently wrong reconstructed schema that every DB rule
// then reasons over.
//
// `type:` is NOT made required by this change; "" remains valid to the loader.
// The defect is the instruction, so the instruction is what gets fixed: name the
// key, and name what happens when it is left out.
func TestRenderConfig_CommentedExampleNamesTypeAndItsConsequence(t *testing.T) {
	comments := configComments(renderOrFail(t, undetectedInfo(t)))
	low := strings.ToLower(comments)

	// Both keys are looked for as EXAMPLE LINES, not as mentions. Measured, not
	// assumed: under the C6 mutation (the `type:` line deleted from the example)
	// a plain strings.Contains(comments, "type:") stayed GREEN, because the
	// paragraph explaining the consequence names `type:` too. A test satisfied by
	// the prose that describes the example cannot notice the example going away.
	if !commentedExampleHasKey(comments, "type") {
		t.Errorf("the commented example must show a `type:` LINE beside schema_paths, or it "+
			"instructs the reader into the silent PostgreSQL default\n--- comments ---\n%s", comments)
	}
	if !commentedExampleHasKey(comments, "schema_paths") {
		t.Errorf("the commented example must still show a `schema_paths:` LINE — it is what turns "+
			"the DB dimension on\n--- comments ---\n%s", comments)
	}

	// The CONSEQUENCE, not just the key. A `type:` line with no explanation reads
	// as optional garnish and gets left out by exactly the reader it protects.
	for _, want := range []string{"postgresql", "mysql", "sqlserver"} {
		if !strings.Contains(low, want) {
			t.Errorf("the comment must name %q so the reader can recognise their own dialect\n"+
				"--- comments ---\n%s", want, comments)
		}
	}
	if !strings.Contains(low, "silently") {
		t.Errorf("the comment must state that omitting type: mis-parses SILENTLY — a wrong parse "+
			"the developer is never told about is the whole risk\n--- comments ---\n%s", comments)
	}
	if !strings.Contains(low, "sqlite") {
		t.Errorf("the comment must name sqlite, which is REFUSED rather than guessed — the one "+
			"dialect that fails loudly instead of quietly\n--- comments ---\n%s", comments)
	}

	// R5's second scenario: naming type: must not cost the honesty about
	// detection that already lives here.
	if !strings.Contains(comments, configGapClaim) || !strings.Contains(low, "migration") {
		t.Errorf("the comment must still state that SQL migration directories are %s\n"+
			"--- comments ---\n%s", configGapClaim, comments)
	}
}

// commentedExampleHasKey reports whether the comment block contains key as a YAML
// KEY LINE of the commented example — a line that, once its `#` is stripped, is
// an indented `key:` — rather than merely mentioning the word somewhere in prose.
//
// The distinction is the whole value of the check: the example is the instruction
// a developer copies, and the paragraph explaining it names the same keys.
func commentedExampleHasKey(comments, key string) bool {
	for _, line := range strings.Split(comments, "\n") {
		body := strings.TrimPrefix(strings.TrimSpace(line), "#")
		if !strings.HasPrefix(body, " ") && !strings.HasPrefix(body, "\t") {
			continue // prose starts flush against the '#'; example lines are indented
		}
		if strings.HasPrefix(strings.TrimSpace(body), key+":") {
			return true
		}
	}
	return false
}

// configComments returns only the comment lines of a rendered config.
func configComments(raw string) string {
	var b strings.Builder
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// liveConfigLines returns the config's non-comment, non-blank lines VERBATIM, in
// order. Comments are dropped because the change this test guards adds prose; the
// order is preserved because a reordering of live keys is exactly what a
// structural comparison of the LOADED config would normalise away.
func liveConfigLines(raw string) []string {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		out = append(out, strings.TrimRight(line, "\r"))
	}
	return out
}

// TestGenerate_PrismaConfigParity is R3, and it is a REGRESSION LOCK, not a RED
// step: it was written and seen green against the unmodified tree before the
// predicate move, so what it pins is today's output, not tomorrow's intention.
//
// The subject is the file `codefit init` really writes — Generate over a copy of
// the real Prisma fixture — because a Prisma project is the one shape that must
// come through the gate move byte-for-byte in its LIVE content.
//
// Two halves, and neither replaces the other:
//   - the loaded config.Database catches a DROPPED key (Q1's rejected
//     alternative deletes `orm:`, and config.Load would happily accept that);
//   - the ordered line golden catches a REORDERING or a re-indentation, which
//     config.Load normalises away into an identical struct.
func TestGenerate_PrismaConfigParity(t *testing.T) {
	root := copyTreeInto(t, sampleNext, filepath.Join(t.TempDir(), "sample-next"))
	if _, err := scaffold.Generate(scaffold.Options{Root: root}); err != nil {
		t.Fatalf("Generate: %v", err)
	}
	written := filepath.Join(root, scaffold.ConfigName)
	raw, err := os.ReadFile(written)
	if err != nil {
		t.Fatalf("reading the config init wrote: %v", err)
	}

	cfg, err := config.Load(written)
	if err != nil {
		t.Fatalf("the config init wrote does not load: %v\n--- yaml ---\n%s", err, raw)
	}
	if cfg.Database.ORM != "prisma" {
		t.Errorf("database.orm = %q, want prisma — a Prisma project must keep every key it has today", cfg.Database.ORM)
	}
	if cfg.Database.Type != "postgresql" {
		t.Errorf("database.type = %q, want postgresql", cfg.Database.Type)
	}
	if cfg.Database.Paradigm != "auto" {
		t.Errorf("database.paradigm = %q, want auto", cfg.Database.Paradigm)
	}
	if !slices.Equal(cfg.Database.SchemaPaths, []string{"prisma/schema.prisma"}) {
		t.Errorf("database.schema_paths = %v, want [prisma/schema.prisma]", cfg.Database.SchemaPaths)
	}

	// The golden is small and lives in the test on purpose: a reviewer reading
	// the diff must be able to see the whole live surface of the artifact,
	// without opening a second file.
	want := []string{
		`version: "1"`,
		`project:`,
		`  name: sample-next`,
		`  language: typescript`,
		`  framework: next`,
		`  path_criticality:`,
		`    production:`,
		`      - app/**`,
		`      - src/**`,
		`    test:`,
		`      - '**/*.test.ts'`,
		`      - '**/*.test.tsx'`,
		`      - '**/*.spec.ts'`,
		`      - '**/*.spec.tsx'`,
		`    example:`,
		`      - examples/**`,
		`      - docs/**`,
		`database:`,
		`  orm: prisma`,
		`  type: postgresql`,
		`  paradigm: auto`,
		`  schema_paths:`,
		`    - prisma/schema.prisma`,
	}
	got := liveConfigLines(string(raw))
	if !slices.Equal(got, want) {
		t.Errorf("the live (non-comment) lines of a Prisma config changed.\n got: %q\nwant: %q\n--- yaml ---\n%s",
			got, want, raw)
	}
}

// A Go project has no ORM/DB: the rendered config must omit the database section
// and still round-trip.
func TestRenderConfigGoRoundTrips(t *testing.T) {
	info := scaffold.ProjectInfo{
		Name:     "codefit",
		Language: "go",
		PathCriticality: config.PathCriticality{
			Production: []string{"**/*.go"},
			Test:       []string{"**/*_test.go"},
		},
	}
	cfg := loadRendered(t, info)
	if cfg.Database.ORM != "" {
		t.Errorf("go project must have no orm, got %q", cfg.Database.ORM)
	}
}
