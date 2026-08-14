package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
	"github.com/codefit-cli/codefit/internal/scaffold"
	"github.com/codefit-cli/codefit/internal/sensors/db"
)

// twoTableDDL declares exactly two tables. The number is MEASURED by
// TestFixture_ReportDDLReallyReconstructs below, through the real parser — a
// fixture is verified by its content, never by its name.
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

func TestFixture_ReportDDLReallyReconstructs(t *testing.T) {
	schema, err := sqlddl.New().ParseSchema([]providers.SourceFile{
		{Path: "V1__init.sql", Content: []byte(twoTableDDL)},
	})
	if err != nil {
		t.Fatalf("the fixture does not parse: %v", err)
	}
	if got := len(schema.Tables); got != 2 {
		t.Fatalf("the fixture reconstructs %d tables, not 2", got)
	}
}

// schemaFixtureRoot builds a root holding a manifest codefit registers no
// provider for, plus the named files. The prefix is asserted.
func schemaFixtureRoot(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("resolving fixture root: %v", err)
	}
	if !strings.Contains(strings.ToLower(abs), "temp") && !strings.Contains(abs, "tmp") {
		t.Fatalf("fixture root %q is not under a temp directory", abs)
	}
	mustWrite(t, filepath.Join(root, "pom.xml"), "<project/>\n")
	for rel, body := range files {
		host := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(host), 0o755); err != nil {
			t.Fatalf("creating %s: %v", rel, err)
		}
		mustWrite(t, host, body)
	}
	return root
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}

// TestInitReport_StatesTheProof is R2's report half. The developer has to be
// able to see WHAT codefit measured, not just that a key appeared: a path and a
// table count are checkable; "schema detected" is not.
func TestInitReport_StatesTheProof(t *testing.T) {
	root := schemaFixtureRoot(t, map[string]string{
		"db/migrations/V1__init.sql": twoTableDDL,
	})
	out, err := runInit(t, root, "", "--non-interactive")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}

	if !strings.Contains(out, "db/migrations") {
		t.Errorf("the report must name the path codefit proved\n---\n%s", out)
	}
	if !strings.Contains(out, "2 table") {
		t.Errorf("the report must state the table count the proof produced — a count is "+
			"checkable, a claim of detection is not\n---\n%s", out)
	}
}

// TestInitReport_NamesTheDialectTheProofRanUnder is residual risk 1's control.
//
// codefit does not sniff the SQL dialect, so a proof with no `database.type`
// configured ran under the DEFAULT binding: PostgreSQL. The proof says the DDL
// RECONSTRUCTS; it does not say the dialect is right. A developer told only
// "proved" would reasonably read it as the stronger claim.
//
// The obvious edit this control exists to catch is "the report is long, trim
// it" — deleting the sentence leaves a report that is shorter, still true in
// every word, and silently over-claims.
func TestInitReport_NamesTheDialectTheProofRanUnder(t *testing.T) {
	root := schemaFixtureRoot(t, map[string]string{
		"db/migrations/V1__init.sql": twoTableDDL,
	})
	out, err := runInit(t, root, "", "--non-interactive")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
	low := strings.ToLower(out)

	if !strings.Contains(low, "postgresql") {
		t.Errorf("the report must NAME the dialect the proof ran under\n---\n%s", out)
	}
	if !strings.Contains(low, "does not sniff") && !strings.Contains(low, "did not measure") {
		t.Errorf("the report must say codefit did not measure the dialect — otherwise "+
			"\"proved\" reads as a claim about the dialect too\n---\n%s", out)
	}
	if !strings.Contains(out, "database.type") {
		t.Errorf("the report must name the key the developer sets to fix it\n---\n%s", out)
	}
}

// TestInitReport_UnprovableCandidateIsNamedWithItsReason is R3's report half.
func TestInitReport_UnprovableCandidateIsNamedWithItsReason(t *testing.T) {
	root := schemaFixtureRoot(t, map[string]string{
		"sql/schema/1_init.up.sql":   twoTableDDL,
		"sql/schema/10_later.up.sql": "CREATE TABLE audit (id BIGINT PRIMARY KEY);\n",
	})
	out, err := runInit(t, root, "", "--non-interactive")
	if err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}

	if !strings.Contains(out, "sql/schema") {
		t.Errorf("the report must name the real directory codefit found\n---\n%s", out)
	}
	if !strings.Contains(out, scaffold.ReasonOrderNotProven) {
		t.Errorf("the report must state the reason %q\n---\n%s", scaffold.ReasonOrderNotProven, out)
	}
	if strings.Contains(out, reportGapClaim) {
		t.Errorf("codefit DID find SQL here, so the report must not also claim it %q\n---\n%s",
			reportGapClaim, out)
	}
}

// TestInitReport_DemotionIsAnnounced is C-R9.
//
// `init --force` re-derives schema_paths from disk. A path that no longer proves
// is DROPPED — and a silent drop is the failure mode the autonomy rule exists to
// prevent: the developer would find out from a scan that suddenly measures
// nothing, with nothing connecting it to the init they just ran.
func TestInitReport_DemotionIsAnnounced(t *testing.T) {
	root := schemaFixtureRoot(t, map[string]string{
		"db/migrations/V1__init.sql": twoTableDDL,
	})
	if out, err := runInit(t, root, "", "--non-interactive"); err != nil {
		t.Fatalf("first init: %v\n%s", err, out)
	}
	raw, err := os.ReadFile(filepath.Join(root, scaffold.ConfigName))
	if err != nil {
		t.Fatalf("reading the config the first init wrote: %v", err)
	}
	if !strings.Contains(string(raw), "- db/migrations") {
		t.Fatalf("the first init must write a live schema_paths, or this test proves nothing "+
			"about a demotion\n---\n%s", raw)
	}

	// The schema goes away. This is the real shape: a migration directory moved,
	// renamed, or emptied between two runs.
	if err := os.Remove(filepath.Join(root, "db", "migrations", "V1__init.sql")); err != nil {
		t.Fatalf("removing the migration: %v", err)
	}

	out, err := runInit(t, root, "", "--force")
	if err != nil {
		t.Fatalf("init --force: %v\n%s", err, out)
	}

	after, err := os.ReadFile(filepath.Join(root, scaffold.ConfigName))
	if err != nil {
		t.Fatalf("reading the regenerated config: %v", err)
	}
	if strings.Contains(string(after), "- db/migrations") {
		t.Errorf("--force must RE-PROVE from disk; the old path was carried over\n---\n%s", after)
	}
	if !strings.Contains(out, "db/migrations") {
		t.Errorf("the report must NAME the path it dropped — a silent demotion is found out "+
			"from a scan that measures nothing\n---\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "dropped") {
		t.Errorf("the report must say the path was DROPPED, and why\n---\n%s", out)
	}
}

// TestInitProofDoesNotBindScanTime is C-R8, and it is a COMPOSITION control: it
// drives the real `init`, then the real DB sensor over the config that init
// wrote, in one test.
//
// Init proving a path is a fact about the tree at that moment. The scan reads the
// tree later, and ADR 0072's floor must still fire when a configured path
// resolves to zero readable schema files — a config codefit wrote itself buys no
// exemption. If it did, the one path most likely to be trusted would be the one
// path with no floor under it.
//
// Neither half alone proves this. The scaffold tests prove init writes a path;
// the sensor tests prove the floor fires for a hand-written config. Only running
// them against each other proves the floor still fires for a path INIT chose.
func TestInitProofDoesNotBindScanTime(t *testing.T) {
	root := schemaFixtureRoot(t, map[string]string{
		"db/migrations/V1__init.sql": twoTableDDL,
	})
	if out, err := runInit(t, root, "", "--non-interactive"); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}

	cfgPath := filepath.Join(root, scaffold.ConfigName)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("the config init wrote does not load: %v", err)
	}
	if len(cfg.Database.SchemaPaths) != 1 || cfg.Database.SchemaPaths[0] != "db/migrations" {
		t.Fatalf("init did not write the proving path; schema_paths = %v. Without it this test "+
			"would assert the floor over a config that configures nothing",
			cfg.Database.SchemaPaths)
	}

	// The tree changes AFTER init proved it. This is the ordinary shape: the
	// migration directory is emptied, moved, or its files renamed.
	if err := os.Remove(filepath.Join(root, "db", "migrations", "V1__init.sql")); err != nil {
		t.Fatalf("removing the migration: %v", err)
	}

	res, err := db.New(sqlddl.New()).Audit(auditctx.AuditContext{ProjectRoot: root, Config: cfg})
	if err != nil {
		t.Fatalf("Audit over the config init wrote must not error: %v", err)
	}
	if res.Measured {
		t.Errorf("the configured path now resolves to ZERO schema files, yet the scan reports "+
			"Measured=true with score %d. A path init PROVED is still only proven for the tree it "+
			"read; a clean bill of health over content codefit never opened is the same defect "+
			"whether the path was written by hand or by init; note: %q", res.Res.Score, res.Note)
	}
	if res.Res.Score != 0 {
		t.Errorf("a not-measured result must publish no score, got %d", res.Res.Score)
	}
	if !strings.Contains(res.Note, "db/migrations") {
		t.Errorf("the note must name the configured path, so the developer can connect the empty "+
			"result to the config init wrote: %q", res.Note)
	}
}
