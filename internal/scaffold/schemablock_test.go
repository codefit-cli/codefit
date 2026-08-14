package scaffold_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/scaffold"
)

// generateInto runs the REAL init path over root and returns the config it
// wrote. Every control in this file reads bytes one Generate run actually
// produced — never a hand-built ProjectInfo, which could hold a shape no
// detection path can reach.
func generateInto(t *testing.T, root string) (scaffold.Result, string) {
	t.Helper()
	res, err := scaffold.Generate(scaffold.Options{Root: root})
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	return res, mustReadConfig(t, root)
}

// TestConfig_ProvenCandidateBecomesALiveSchemaPaths is C-R2a's config half.
func TestConfig_ProvenCandidateBecomesALiveSchemaPaths(t *testing.T) {
	root := schemaRoot(t, map[string]string{
		"db/migrations/V1__init.sql": twoTableDDL,
	})
	_, cfg := generateInto(t, root)

	if !liveYAMLKeyPresent(cfg, "database") {
		t.Fatalf("a proven schema source must produce a LIVE `database:` block\n---\n%s", cfg)
	}
	if !liveYAMLKeyPresent(cfg, "schema_paths") {
		t.Fatalf("a proven schema source must be written as a live schema_paths key\n---\n%s", cfg)
	}
	if !slices.Contains(liveConfigLines(cfg), "    - db/migrations") {
		t.Errorf("the live schema_paths must name the proven path exactly as discovery spelled "+
			"it\n live lines: %q\n---\n%s", liveConfigLines(cfg), cfg)
	}
}

// TestConfig_ReconstructsNoTableIsNeverLive is C-R2b. An ordered directory that
// reconstructs nothing must NOT be promoted: a live schema_paths over an empty
// model yields measured:true and a score over a schema that does not exist.
func TestConfig_ReconstructsNoTableIsNeverLive(t *testing.T) {
	root := schemaRoot(t, map[string]string{
		"db/migrations/V1__nothing.sql": "-- nothing is declared here\nSELECT 1;\n",
	})
	_, cfg := generateInto(t, root)

	if liveYAMLKeyPresent(cfg, "schema_paths") {
		t.Errorf("a directory the parser made no table of was written live\n---\n%s", cfg)
	}
	comments := configComments(cfg)
	if !strings.Contains(comments, "db/migrations") {
		t.Errorf("the commented block must NAME the real path codefit looked at\n"+
			"--- comments ---\n%s", comments)
	}
	if !strings.Contains(comments, scaffold.ReasonNoTable) {
		t.Errorf("the commented block must state the reason %q\n--- comments ---\n%s",
			scaffold.ReasonNoTable, comments)
	}
}

// TestConfig_MySQLFlavouredDDLIsNeverLive is C-R6b, shaped by the M1
// measurement (see TestProve_MySQLFlavouredDDLDoesNotReconstruct, which pins the
// count of ZERO through the real parser).
//
// The residual risk this change declares is that codefit does not sniff the SQL
// dialect: a directory with no `database.type` set is parsed under the default
// binding, and the default is PostgreSQL. The question that risk turns on is
// whether a MySQL migration set could go LIVE over a partly-wrong model.
//
// It cannot, and this is the control that keeps it so: MySQL-flavoured DDL
// reconstructs zero tables, the proof gate requires at least one, so the
// directory receives the COMMENTED block naming the real path and the reason.
// codefit reaches the correct outcome without ever guessing a dialect.
//
// If a future parser change makes this DDL reduce, this test goes red — and the
// right response is to re-argue the residual, not to relax the assertion.
func TestConfig_MySQLFlavouredDDLIsNeverLive(t *testing.T) {
	const mysqlDDL = "CREATE TABLE `customer` (\n" +
		"  `id` BIGINT NOT NULL AUTO_INCREMENT,\n" +
		"  `email` VARCHAR(255) NOT NULL,\n" +
		"  PRIMARY KEY (`id`)\n" +
		") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;\n"

	root := schemaRoot(t, map[string]string{
		"db/migrations/V1__init.sql": mysqlDDL,
	})
	info := detectOrFail(t, root)
	if c := candidateFor(t, info, "db/migrations"); c.Tables != 0 {
		t.Fatalf("MySQL-flavoured DDL reconstructed %d table(s) under the default binding. The "+
			"dialect residual is no longer closed by non-reconstruction: decide whether the proof "+
			"gate may still promote a directory whose dialect codefit never measured", c.Tables)
	}

	_, cfg := generateInto(t, root)
	if liveYAMLKeyPresent(cfg, "schema_paths") {
		t.Errorf("a dialect codefit cannot reconstruct went live\n---\n%s", cfg)
	}
	if !strings.Contains(configComments(cfg), "db/migrations") {
		t.Errorf("the commented block must still name the real path\n---\n%s", cfg)
	}
}

// TestConfig_LiveBlockNeverInventsTheDialect is C-R6. codefit measured the PATH,
// not the DIALECT, and the config must say exactly that much.
func TestConfig_LiveBlockNeverInventsTheDialect(t *testing.T) {
	root := schemaRoot(t, map[string]string{
		"db/migrations/V1__init.sql": twoTableDDL,
	})
	_, cfg := generateInto(t, root)

	for _, line := range liveConfigLines(cfg) {
		if strings.HasPrefix(strings.TrimSpace(line), "type:") {
			t.Errorf("the config carries a LIVE %q. codefit does not sniff the SQL dialect, so a "+
				"live type: here would be invented — the one thing R6 forbids\n---\n%s",
				strings.TrimSpace(line), cfg)
		}
	}

	comments := configComments(cfg)
	if !commentedExampleHasKey(comments, "type") {
		t.Errorf("a live block must carry a COMMENTED `type:` LINE, not merely prose about the "+
			"dialect — the line is what the developer uncomments\n--- comments ---\n%s", comments)
	}
	low := strings.ToLower(comments)
	for _, want := range []string{"postgresql", "mysql", "sqlserver"} {
		if !strings.Contains(low, want) {
			t.Errorf("the commented type: must name %q so the reader can recognise their own "+
				"dialect\n--- comments ---\n%s", want, comments)
		}
	}
	if !strings.Contains(low, "silently") {
		t.Errorf("the comment must state that omitting type: mis-parses SILENTLY\n"+
			"--- comments ---\n%s", comments)
	}

	// Position matters: a dialect warning three screens away from the key it
	// qualifies is a warning nobody reads. It must sit directly above the live
	// schema_paths the developer is looking at.
	if !commentedTypePrecedesLiveSchemaPaths(cfg) {
		t.Errorf("the commented `type:` line must sit directly above the live `schema_paths:` "+
			"key it qualifies\n---\n%s", cfg)
	}
}

// commentedTypePrecedesLiveSchemaPaths reports whether a commented `type:` line
// appears above the live `schema_paths:` key with no other live key between them.
func commentedTypePrecedesLiveSchemaPaths(raw string) bool {
	sawCommentedType := false
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		body := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
		if strings.HasPrefix(trimmed, "#") {
			if strings.HasPrefix(body, "type:") {
				sawCommentedType = true
			}
			continue
		}
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "schema_paths:") {
			return sawCommentedType
		}
	}
	return false
}

// TestConfig_TwoProvenCandidatesLeaveNoneLive is C-R4.
//
// schema_paths entries MERGE into ONE reconstructed model, so a wrong extra
// entry does not add noise — it poisons the model, and DB-050 then affirms "this
// table has no primary key" at confidence 1.0 about a table that is not there.
// Writing the first of N would be exactly that guess.
func TestConfig_TwoProvenCandidatesLeaveNoneLive(t *testing.T) {
	root := schemaRoot(t, map[string]string{
		"services/billing/db/V1__init.sql": twoTableDDL,
		"services/orders/db/V1__init.sql":  "CREATE TABLE orders (id BIGINT PRIMARY KEY);\n",
	})
	info := detectOrFail(t, root)
	if len(info.SchemaPaths) != 0 {
		t.Errorf("schema_paths = %v, want none — two directories proved and codefit cannot know "+
			"whether they are one schema or several", info.SchemaPaths)
	}

	_, cfg := generateInto(t, root)
	if liveYAMLKeyPresent(cfg, "schema_paths") {
		t.Fatalf("two proven candidates must leave NO live schema_paths\n---\n%s", cfg)
	}
	comments := configComments(cfg)
	for _, want := range []string{"services/billing/db", "services/orders/db"} {
		if !strings.Contains(comments, want) {
			t.Errorf("the commented block must name every proven candidate; %q is missing\n"+
				"--- comments ---\n%s", want, comments)
		}
	}
	// The COUNTS are the reason the developer can choose. A list of paths with no
	// measurement behind it asks them to guess as blindly as codefit refused to.
	if !strings.Contains(comments, "2 table") {
		t.Errorf("the commented block must carry each candidate's proven table count\n"+
			"--- comments ---\n%s", comments)
	}
	if !strings.Contains(comments, "1 table") {
		t.Errorf("the commented block must carry each candidate's proven table count\n"+
			"--- comments ---\n%s", comments)
	}
}

// TestConfig_UnprovableDirectoryIsNamedWithItsReason is C-R3a.
//
// THE FIXTURE PATH IS `sql/schema`, DELIBERATELY NOT `db/migrations`.
// `db/migrations` is the template's OWN invented placeholder. A fixture using it
// would make the "the placeholder must not appear as if it were a finding"
// assertion below pass for the exact reason the assertion exists to forbid: the
// string would be present either way, and the test could never tell a real
// finding from the hardcoded example. A different path is what gives the
// assertion its discriminating power.
func TestConfig_UnprovableDirectoryIsNamedWithItsReason(t *testing.T) {
	root := schemaRoot(t, map[string]string{
		"sql/schema/1_init.up.sql":   twoTableDDL,
		"sql/schema/1_init.down.sql": "DROP TABLE invoice;\n",
		"sql/schema/10_later.up.sql": "CREATE TABLE audit (id BIGINT PRIMARY KEY);\n",
	})
	_, cfg := generateInto(t, root)

	if liveYAMLKeyPresent(cfg, "schema_paths") {
		t.Fatalf("golang-migrate naming must never reach a live schema_paths\n---\n%s", cfg)
	}
	comments := configComments(cfg)
	if !strings.Contains(comments, "sql/schema") {
		t.Errorf("the commented block must name the REAL directory codefit found\n"+
			"--- comments ---\n%s", comments)
	}
	if !strings.Contains(comments, scaffold.ReasonOrderNotProven) {
		t.Errorf("the commented block must state that codefit cannot prove the apply order of "+
			"those filenames\n--- comments ---\n%s", comments)
	}
	// THE DISCRIMINATING PAIR, and the reason the fixture path is not
	// `db/migrations`.
	//
	// The commented example is the line a developer copies. With a real path in
	// hand it must carry THAT path, and the invented placeholder must be gone.
	// Both assertions below are satisfied trivially by a fixture living at
	// `db/migrations`: the template's hardcoded placeholder supplies the same
	// string, so the test could not tell an example from a finding — it would
	// pass for the exact reason it exists to forbid.
	if !commentedExampleNames(comments, "sql/schema") {
		t.Errorf("the commented example must name the REAL path codefit found, not a hardcoded "+
			"one. This is the line the developer copies\n--- comments ---\n%s", comments)
	}
	if strings.Contains(cfg, "db/migrations") {
		t.Errorf("codefit found a real path (sql/schema) yet the config still carries the invented "+
			"placeholder \"db/migrations\". A developer cannot tell an example from a finding when "+
			"both are printed the same way\n---\n%s", cfg)
	}
}

// commentedExampleNames reports whether the commented block's schema_paths
// EXAMPLE — a commented list entry under a commented `schema_paths:` key —
// names path.
//
// It looks at the example ENTRY rather than anywhere in the prose, because the
// prose above it also names every path codefit found. A check satisfied by the
// explanation cannot notice the instruction going wrong, which is the C6 failure
// this project has already paid for once.
func commentedExampleNames(comments, path string) bool {
	inExample := false
	for _, line := range strings.Split(comments, "\n") {
		body := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
		if strings.HasPrefix(body, "schema_paths:") {
			inExample = true
			continue
		}
		if inExample {
			if !strings.HasPrefix(body, "-") {
				inExample = false
				continue
			}
			if strings.Contains(body, path) {
				return true
			}
		}
	}
	return false
}

// TestConfig_NoSQLAnywhereSaysCodefitLookedAndFoundNone is C-R3b. Absence is
// first-class, and its declaration has to distinguish "codefit looked and found
// nothing" from "codefit did not look" — and must name no project-specific path,
// because there is none to name.
func TestConfig_NoSQLAnywhereSaysCodefitLookedAndFoundNone(t *testing.T) {
	root := schemaRoot(t, map[string]string{
		"src/main/java/App.java": "class App {}\n",
	})
	_, cfg := generateInto(t, root)

	if liveYAMLKeyPresent(cfg, "database") {
		t.Fatalf("no schema source exists, so no live `database:` block may be written\n---\n%s", cfg)
	}
	comments := configComments(cfg)
	low := strings.ToLower(comments)

	if !strings.Contains(low, "found none") && !strings.Contains(low, "found no ") {
		t.Errorf("the block must say codefit LOOKED and found none — silence is indistinguishable "+
			"from not having looked\n--- comments ---\n%s", comments)
	}
	// The DECLARED BOUND. Without it, "no schema source found" can be mistaken
	// for "this project has none", and a schema seven levels down is invisible
	// with no way to know why.
	if !strings.Contains(comments, "6") {
		t.Errorf("the block must declare how deep codefit walked, so a schema below the bound is "+
			"a stated limit rather than a silent miss\n--- comments ---\n%s", comments)
	}
	// The example must READ as an example. This is the assertion the whole
	// placeholder problem turns on.
	if !commentedExampleHasKey(comments, "schema_paths") {
		t.Errorf("the block must still show a commented schema_paths example — it is how the "+
			"dimension gets turned on by hand\n--- comments ---\n%s", comments)
	}
	if !strings.Contains(low, "example") {
		t.Errorf("with nothing found, the commented path must be marked as an EXAMPLE rather than "+
			"printed like a discovered path\n--- comments ---\n%s", comments)
	}
}

// TestConfig_PrismaGainsNoCommentedType extends the Prisma parity golden in the
// one direction the golden CANNOT cover: its ordered-line comparison reads
// non-comment lines only, so a stray comment leaking into the Prisma case would
// pass it unnoticed. Prisma MEASURES its datasource provider, so its `type:` is
// live and a commented one beside it would contradict the file it sits in.
func TestConfig_PrismaGainsNoCommentedType(t *testing.T) {
	root := copyTree(t, sampleNext)
	_, cfg := generateInto(t, root)

	if !slices.Contains(liveConfigLines(cfg), "  type: postgresql") {
		t.Fatalf("the Prisma fixture must keep its LIVE measured type:\n---\n%s", cfg)
	}
	if commentedExampleHasKey(configComments(cfg), "type") {
		t.Errorf("a Prisma config measured its dialect, so it must NOT also carry a commented "+
			"`type:` line telling the developer codefit did not measure one\n---\n%s", cfg)
	}
}
