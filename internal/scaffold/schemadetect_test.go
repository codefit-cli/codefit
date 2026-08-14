package scaffold_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
	"github.com/codefit-cli/codefit/internal/scaffold"
	"github.com/codefit-cli/codefit/internal/schemasource"
)

// twoTableDDL declares exactly two tables. The count is not taken on trust:
// TestFixture_TwoTableDDLReallyReconstructs feeds this exact string to the REAL
// sqlddl parser and asserts the number, so every test below that relies on "this
// directory proves" is standing on a measured fixture rather than on a
// `CREATE TABLE` a grep found in a comment.
const twoTableDDL = `
CREATE TABLE customer (
  id      BIGINT PRIMARY KEY,
  email   VARCHAR(255) NOT NULL
);
CREATE TABLE invoice (
  id          BIGINT PRIMARY KEY,
  customer_id BIGINT NOT NULL REFERENCES customer(id)
);
`

// TestFixture_TwoTableDDLReallyReconstructs is the fixture's own verification.
// A fixture is verified by its CONTENT, not by its name: if this string ever
// stops reconstructing two tables, every test in this file that claims a
// directory "proves" would be passing for the wrong reason.
func TestFixture_TwoTableDDLReallyReconstructs(t *testing.T) {
	schema, err := sqlddl.New().ParseSchema([]providers.SourceFile{
		{Path: "V1__init.sql", Content: []byte(twoTableDDL)},
	})
	if err != nil {
		t.Fatalf("the fixture does not parse: %v", err)
	}
	if got := len(schema.Tables); got != 2 {
		t.Fatalf("the twoTableDDL fixture reconstructs %d tables, not 2 — every 'this directory "+
			"proves' assertion in this file is built on that number", got)
	}
}

// schemaRoot builds a fixture project root: a build manifest codefit registers
// NO provider for, plus whatever files the caller names. The root's prefix is
// asserted, per the project's fixture rule.
func schemaRoot(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("resolving fixture root: %v", err)
	}
	if !strings.Contains(strings.ToLower(abs), "temp") && !strings.Contains(abs, "tmp") {
		t.Fatalf("fixture root %q is not under a temp directory", abs)
	}
	writeFile(t, root, "pom.xml", "<project/>\n")
	for rel, body := range files {
		host := filepath.FromSlash(rel)
		if dir := filepath.Dir(host); dir != "." {
			mkdirs(t, root, dir)
		}
		writeFile(t, root, host, body)
	}
	return root
}

// detectOrFail runs the REAL detection path over root.
func detectOrFail(t *testing.T, root string) scaffold.ProjectInfo {
	t.Helper()
	info, err := scaffold.Detect(root)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	return info
}

// candidatePaths lists the discovered candidates by path, for readable failures.
func candidatePaths(info scaffold.ProjectInfo) []string {
	out := make([]string, 0, len(info.SchemaCandidates))
	for _, c := range info.SchemaCandidates {
		out = append(out, c.Path)
	}
	return out
}

// candidateFor returns the discovered candidate with that path.
func candidateFor(t *testing.T, info scaffold.ProjectInfo, path string) scaffold.SchemaCandidate {
	t.Helper()
	for _, c := range info.SchemaCandidates {
		if c.Path == path {
			return c
		}
	}
	t.Fatalf("no candidate named %q; discovery found %v", path, candidatePaths(info))
	return scaffold.SchemaCandidate{}
}

// TestDetect_FlywayRootWithNoProvider is C-R1a. The headline case: a build
// manifest codefit registers no provider for, and real Flyway migrations beside
// it. Detection of the schema must not be gated on detection of the language.
func TestDetect_FlywayRootWithNoProvider(t *testing.T) {
	root := schemaRoot(t, map[string]string{
		"db/migrations/V1__init.sql": twoTableDDL,
	})
	info := detectOrFail(t, root)

	if info.Detected() {
		t.Fatalf("the fixture must stay the undetected case; language = %q", info.Language)
	}
	if !slices.Equal(info.SchemaPaths, []string{"db/migrations"}) {
		t.Errorf("schema_paths = %v, want [db/migrations] — a schema source is orthogonal to the "+
			"application language (ADR 0018), so an undetected root must still get its schema",
			info.SchemaPaths)
	}
	if got := candidateFor(t, info, "db/migrations"); got.Tables != 2 {
		t.Errorf("candidate tables = %d, want 2 (measured through the real parser)", got.Tables)
	}
}

// TestDetect_GolangMigrateIsNeverLive is C-R1b. golang-migrate's `10_x` sorts
// before `1_init`, so the apply order of that directory is a fact about the
// alphabet. codefit must name the directory and refuse to write it.
func TestDetect_GolangMigrateIsNeverLive(t *testing.T) {
	root := schemaRoot(t, map[string]string{
		"db/migrations/1_init.up.sql":   twoTableDDL,
		"db/migrations/1_init.down.sql": "DROP TABLE invoice;\n",
		"db/migrations/10_x.up.sql":     "CREATE TABLE audit (id BIGINT PRIMARY KEY);\n",
	})
	info := detectOrFail(t, root)

	if len(info.SchemaPaths) != 0 {
		t.Errorf("schema_paths = %v, want none — codefit cannot prove this directory's apply order",
			info.SchemaPaths)
	}
	c := candidateFor(t, info, "db/migrations")
	if c.Reason != scaffold.ReasonOrderNotProven {
		t.Errorf("reason = %q, want %q — the developer has to learn WHY, not just that nothing "+
			"was written", c.Reason, scaffold.ReasonOrderNotProven)
	}
}

// TestDetect_OneStrayFileDisqualifies is C-R1c. A name the resolver cannot
// version lands in its LEXICAL bucket, and from that point the whole level's
// apply order is unproven — so one stray file is enough.
func TestDetect_OneStrayFileDisqualifies(t *testing.T) {
	root := schemaRoot(t, map[string]string{
		"db/migrations/V1__a.sql": twoTableDDL,
		"db/migrations/seed.sql":  "INSERT INTO customer (id, email) VALUES (1, 'a@b.c');\n",
	})
	info := detectOrFail(t, root)

	if len(info.SchemaPaths) != 0 {
		t.Errorf("schema_paths = %v, want none — seed.sql is unversioned, so the level's order "+
			"is lexical", info.SchemaPaths)
	}
	if c := candidateFor(t, info, "db/migrations"); c.Reason != scaffold.ReasonOrderNotProven {
		t.Errorf("reason = %q, want %q", c.Reason, scaffold.ReasonOrderNotProven)
	}
}

// TestDetect_DottedVersionIsNotProven is C-R1d. `V1.1__a.sql` is a real Flyway
// spelling the resolver's ^V(\d+)__ does not version.
func TestDetect_DottedVersionIsNotProven(t *testing.T) {
	root := schemaRoot(t, map[string]string{
		"db/migrations/V1.1__a.sql": twoTableDDL,
	})
	info := detectOrFail(t, root)

	if len(info.SchemaPaths) != 0 {
		t.Errorf("schema_paths = %v, want none — V1.1__ is not an integer version", info.SchemaPaths)
	}
	if c := candidateFor(t, info, "db/migrations"); c.Reason != scaffold.ReasonOrderNotProven {
		t.Errorf("reason = %q, want %q", c.Reason, scaffold.ReasonOrderNotProven)
	}
}

// TestDetect_NestedTreePromotesTheInnerDirectoryOnly is C-R5, and it is the
// control that keeps discovery depth from being mistaken for read depth.
//
// db/nested holds no .sql at its OWN level. The scan-time resolver reads exactly
// one level, so writing db/nested would name a directory that resolves to zero
// files — ADR 0072's floor, reached through a config codefit wrote itself.
func TestDetect_NestedTreePromotesTheInnerDirectoryOnly(t *testing.T) {
	root := schemaRoot(t, map[string]string{
		"db/nested/sql/V1__a.sql": twoTableDDL,
	})
	info := detectOrFail(t, root)

	if slices.Contains(info.SchemaPaths, "db/nested") {
		t.Errorf("db/nested was written live, but it holds no .sql at its own level — the "+
			"resolver would read it as empty. schema_paths = %v", info.SchemaPaths)
	}
	if slices.Contains(candidatePaths(info), "db/nested") {
		t.Errorf("db/nested became a candidate by inheriting from a descendant; candidacy is "+
			"per DIRECTORY. candidates = %v", candidatePaths(info))
	}
	if !slices.Equal(info.SchemaPaths, []string{"db/nested/sql"}) {
		t.Errorf("schema_paths = %v, want [db/nested/sql]", info.SchemaPaths)
	}
}

// TestDetect_DiscoveryDepthBound is C-D5. Flyway's canonical JVM location is
// src/main/resources/db/migration — five levels below the root — so a bound that
// misses it would miss the exact case this change exists to serve. Depth 7 is
// the other side of the same declared number.
func TestDetect_DiscoveryDepthBound(t *testing.T) {
	t.Run("depth 5 is found", func(t *testing.T) {
		root := schemaRoot(t, map[string]string{
			"src/main/resources/db/migration/V1__init.sql": twoTableDDL,
		})
		info := detectOrFail(t, root)
		if !slices.Equal(info.SchemaPaths, []string{"src/main/resources/db/migration"}) {
			t.Errorf("schema_paths = %v, want [src/main/resources/db/migration] — Flyway's "+
				"canonical JVM layout sits at depth 5 and is the headline case", info.SchemaPaths)
		}
	})

	t.Run("depth 7 is not found", func(t *testing.T) {
		root := schemaRoot(t, map[string]string{
			"a/b/c/d/e/f/g/V1__init.sql": twoTableDDL,
		})
		info := detectOrFail(t, root)
		if len(info.SchemaPaths) != 0 {
			t.Errorf("schema_paths = %v, want none — depth 7 is past the declared bound",
				info.SchemaPaths)
		}
		if len(info.SchemaCandidates) != 0 {
			t.Errorf("candidates = %v, want none past the bound", candidatePaths(info))
		}
	})
}

// TestDiscovery_SkipsBuildOutputTwin is C-D6, and it keeps the HEADLINE CASE
// working on a project that has been compiled.
//
// Maven copies src/main/resources/** into target/classes/**. Without pruning
// build output, a built Flyway project has TWO proven directories with identical
// DDL, R4 fires, and the developer gets no live block on a perfectly normal
// project — an answer nobody could act on.
func TestDiscovery_SkipsBuildOutputTwin(t *testing.T) {
	root := schemaRoot(t, map[string]string{
		"src/main/resources/db/migration/V1__init.sql": twoTableDDL,
		"target/classes/db/migration/V1__init.sql":     twoTableDDL,
	})
	info := detectOrFail(t, root)

	for _, p := range candidatePaths(info) {
		if strings.HasPrefix(p, "target/") {
			t.Errorf("discovery counted the build-output twin %q; a compiled project would then "+
				"have two proven candidates and get no live block at all", p)
		}
	}
	if !slices.Equal(info.SchemaPaths, []string{"src/main/resources/db/migration"}) {
		t.Errorf("schema_paths = %v, want [src/main/resources/db/migration] — the source copy, "+
			"with the build twin pruned", info.SchemaPaths)
	}
}

// TestDiscovery_SkipsEveryDirsToSkipName locks the UNION RELATION through the
// real walk rather than through a map literal.
//
// discoverySkipDirs is dirsToSkip ∪ buildOutputDirs, and the invariant is that
// nothing already too vendored or too built to count route handlers in can ever
// become a schema source. Asserting the two maps against each other would pass
// for a walk that ignored both; planting a PROVING migration directory under
// each skipped name and driving real detection cannot.
func TestDiscovery_SkipsEveryDirsToSkipName(t *testing.T) {
	// The names are read back from the package rather than retyped, so a name
	// added to dirsToSkip is covered here the day it is added.
	for _, name := range scaffold.SkippedDiscoveryDirNames() {
		t.Run(name, func(t *testing.T) {
			root := schemaRoot(t, map[string]string{
				name + "/db/migrations/V1__init.sql": twoTableDDL,
			})
			info := detectOrFail(t, root)
			if len(info.SchemaCandidates) != 0 {
				t.Errorf("discovery walked into %q and found %v; skipped directories hold copies "+
					"and vendored trees, never this project's schema", name, candidatePaths(info))
			}
			if len(info.SchemaPaths) != 0 {
				t.Errorf("schema_paths = %v was taken from %q", info.SchemaPaths, name)
			}
		})
	}
}

// TestDiscovery_UnionCoversDirsToSkip is the relation itself, stated once. The
// behavioural lock above is the control; this one names the invariant so a
// future reader sees WHY discovery skips more than route counting does.
func TestDiscovery_UnionCoversDirsToSkip(t *testing.T) {
	skipped := scaffold.SkippedDiscoveryDirNames()
	for _, name := range scaffold.RouteWalkSkippedDirNames() {
		if !slices.Contains(skipped, name) {
			t.Errorf("%q is skipped when counting route handlers but not when discovering a "+
				"schema. The two lists have drifted: discovery must skip at least everything the "+
				"route walk skips", name)
		}
	}
}

// TestDiscovery_NeverPromotesATestFixture is a REGRESSION control, and it comes
// from codefit auditing itself.
//
// The first run of this feature over codefit's own repository walked five
// candidate directories under testdata, proved exactly one — a two-table Flyway
// fixture written to exercise the DB sensor — and wrote it into codefit's
// .codefit.yaml as codefit's schema. Exactly one proved, so the ambiguity rule
// stayed silent, and the result was a confident, measured, WRONG answer of
// precisely the kind this change exists to remove.
//
// The fixture below is the same shape: a project whose only proven DDL lives
// under testdata.
func TestDiscovery_NeverPromotesATestFixture(t *testing.T) {
	root := schemaRoot(t, map[string]string{
		"internal/store/testdata/migrations/V1__init.sql": twoTableDDL,
	})
	info := detectOrFail(t, root)

	if len(info.SchemaPaths) != 0 {
		t.Errorf("schema_paths = %v — that is TEST DATA, not this project's schema. Auditing a "+
			"fixture while reporting a measured schema is the confident wrong answer, and it is "+
			"worse than finding nothing", info.SchemaPaths)
	}
	if len(info.SchemaCandidates) != 0 {
		t.Errorf("candidates = %v, want none: testdata is pruned, not merely left unpromoted",
			candidatePaths(info))
	}
}

// TestDiscovery_CodefitDoesNotDetectItsOwnFixturesAsItsSchema drives the REAL
// detection over the REAL repository, which is the only way to prove the
// regression above is actually fixed in the tree that ships.
//
// A synthetic fixture can only reproduce the shape someone remembered. This runs
// the walk over the same directory layout codefit dogfoods `init` on, and it is
// the probe that caught the defect in the first place.
func TestDiscovery_CodefitDoesNotDetectItsOwnFixturesAsItsSchema(t *testing.T) {
	root := repoRoot(t)
	info := detectOrFail(t, root)

	if !info.Detected() {
		t.Fatalf("Detect(%s) resolved no language (%q) — the probe is broken, not the walk",
			root, info.Language)
	}
	// The positive control for this test's own machinery: the repository really
	// does hold SQL that WOULD be found if it were not pruned. Without this, a
	// walk that silently stopped working would make the assertion below pass.
	if !repoHoldsProvableFixtureDDL(t, root) {
		t.Fatal("this repository no longer holds a provable SQL fixture, so the assertion below " +
			"would pass whether or not fixtures are pruned")
	}
	if len(info.SchemaPaths) != 0 {
		t.Errorf("codefit init would write %v into codefit's own config. Those are test fixtures "+
			"for the DB sensor, not codefit's schema", info.SchemaPaths)
	}
}

// repoHoldsProvableFixtureDDL confirms, through the REAL prover, that this
// repository still contains a fixture directory that WOULD prove if discovery
// looked at it. It is the positive control for the test above.
func repoHoldsProvableFixtureDDL(t *testing.T, root string) bool {
	t.Helper()
	const known = "internal/sensors/db/testdata/migrations_traceless"
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(known))); err != nil {
		return false
	}
	p, err := schemasource.Prove(root, known)
	if err != nil {
		t.Fatalf("proving the known fixture %q: %v", known, err)
	}
	return p.Ordered && p.Tables > 0
}

// TestDetect_NoSQLAnywhereFindsNothing is the absence case, and it is here as a
// POSITIVE CONTROL for every "want none" assertion above: those pass trivially
// if discovery is broken, so this file also proves discovery FINDS things
// (TestDetect_FlywayRootWithNoProvider) and that an empty result is an empty
// project rather than a dead walk.
func TestDetect_NoSQLAnywhereFindsNothing(t *testing.T) {
	root := schemaRoot(t, map[string]string{
		"src/main/java/App.java": "class App {}\n",
	})
	info := detectOrFail(t, root)

	if len(info.SchemaCandidates) != 0 {
		t.Errorf("candidates = %v, want none in a project with no .sql", candidatePaths(info))
	}
	if len(info.SchemaPaths) != 0 {
		t.Errorf("schema_paths = %v, want none", info.SchemaPaths)
	}
}

// TestDetect_PrismaKeepsItsOwnSchema pins the interaction with the ONE detection
// path that already existed. A .prisma and a .sql path in one schema_paths
// resolve NO parser at all, so a Prisma project must not acquire a second entry.
func TestDetect_PrismaKeepsItsOwnSchema(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "package.json", `{"dependencies":{"next":"14.0.0"}}`)
	mkdirs(t, root, "prisma")
	writeFile(t, root, filepath.Join("prisma", "schema.prisma"),
		"datasource db {\n  provider = \"postgresql\"\n}\n")
	mkdirs(t, root, filepath.Join("db", "migrations"))
	writeFile(t, root, filepath.Join("db", "migrations", "V1__init.sql"), twoTableDDL)

	info := detectOrFail(t, root)
	if !slices.Equal(info.SchemaPaths, []string{filepath.FromSlash("prisma/schema.prisma")}) {
		t.Errorf("schema_paths = %v, want the Prisma schema alone — mixing .prisma and .sql in "+
			"one schema_paths resolves no parser", info.SchemaPaths)
	}
}

// mustReadConfig reads back the .codefit.yaml a Generate run wrote.
func mustReadConfig(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, scaffold.ConfigName))
	if err != nil {
		t.Fatalf("reading the config init wrote: %v", err)
	}
	return string(raw)
}
