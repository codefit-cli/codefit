package schemasource

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// postgresTwoTableDDL declares exactly two tables. Every fixture in this file
// that claims a table count is fed to the REAL parser by the test that uses it —
// never grepped for `CREATE TABLE`, which would count a string in a comment.
const postgresTwoTableDDL = `
CREATE TABLE customer (
  id      BIGINT PRIMARY KEY,
  email   VARCHAR(255) NOT NULL
);
CREATE TABLE invoice (
  id          BIGINT PRIMARY KEY,
  customer_id BIGINT NOT NULL REFERENCES customer(id)
);
`

// mysqlFlavouredDDL is the M1 fixture: backtick identifiers, AUTO_INCREMENT and
// an ENGINE clause — MySQL's spelling, fed to the parser codefit binds when
// database.type is UNSET. See TestProve_MySQLFlavouredDDLDoesNotReconstruct for
// what it measures and why the measurement is the control.
const mysqlFlavouredDDL = "CREATE TABLE `customer` (\n" +
	"  `id` BIGINT NOT NULL AUTO_INCREMENT,\n" +
	"  `email` VARCHAR(255) NOT NULL,\n" +
	"  PRIMARY KEY (`id`)\n" +
	") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;\n" +
	"CREATE TABLE `invoice` (\n" +
	"  `id` BIGINT NOT NULL AUTO_INCREMENT,\n" +
	"  `customer_id` BIGINT NOT NULL,\n" +
	"  PRIMARY KEY (`id`)\n" +
	") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;\n"

// writeDDL creates root/rel/name holding body, and returns the slash-spelled
// project-relative directory — the exact spelling that would be written into
// database.schema_paths.
func writeDDL(t *testing.T, root, rel string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("creating %s: %v", rel, err)
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("writing %s/%s: %v", rel, name, err)
		}
	}
	return rel
}

// tempRoot returns a t.TempDir with its prefix asserted, per the project's
// fixture rule: a fixture root must be provably inside the scratch tree.
func tempRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatalf("resolving fixture root: %v", err)
	}
	if !strings.HasPrefix(abs, filepath.Clean(os.TempDir())) &&
		!strings.Contains(abs, "Temp") && !strings.Contains(abs, "tmp") {
		t.Fatalf("fixture root %q is not under a temp directory", abs)
	}
	return root
}

// TestProve_ReconstructsThroughTheRealParser is R2's unit half: Prove promotes
// nothing on its own, it MEASURES. The table count must come from the parser
// codefit really binds, so this test asserts the count rather than a boolean.
func TestProve_ReconstructsThroughTheRealParser(t *testing.T) {
	root := tempRoot(t)
	rel := writeDDL(t, root, "db/migrations", map[string]string{
		"V1__init.sql": postgresTwoTableDDL,
	})

	p, err := Prove(root, rel)
	if err != nil {
		t.Fatalf("Prove: %v", err)
	}
	if !p.Ordered {
		t.Errorf("a lone V1__init.sql is Flyway-ordered; Ordered = false")
	}
	if p.SQLFiles != 1 {
		t.Errorf("SQLFiles = %d, want 1", p.SQLFiles)
	}
	if p.Tables != 2 {
		t.Errorf("Tables = %d, want 2 — the fixture declares two tables and the proof must "+
			"come from the real parser, not from the filename", p.Tables)
	}
}

// TestProve_ReconstructsNoTable is R2's second scenario. A directory can be
// perfectly ordered and still reconstruct nothing; promoting it would write a
// schema_paths the audit then measures as an empty model.
func TestProve_ReconstructsNoTable(t *testing.T) {
	root := tempRoot(t)
	rel := writeDDL(t, root, "db/migrations", map[string]string{
		"V1__nothing.sql": "-- a comment, and a query that declares nothing\nSELECT 1;\n",
	})

	p, err := Prove(root, rel)
	if err != nil {
		t.Fatalf("Prove: %v", err)
	}
	if !p.Ordered {
		t.Errorf("Ordered = false for a single V1__ file")
	}
	if p.Tables != 0 {
		t.Errorf("Tables = %d, want 0 — comments and a SELECT declare no table", p.Tables)
	}
}

// TestProve_UnorderedDirectoryIsNotParsed pins the short-circuit. golang-migrate
// naming cannot reach the parser at all: if it did, a directory whose apply
// order is a fact about the alphabet could still be promoted on a table count
// produced in the wrong order.
func TestProve_UnorderedDirectoryIsNotParsed(t *testing.T) {
	root := tempRoot(t)
	rel := writeDDL(t, root, "db/migrations", map[string]string{
		"1_init.up.sql":   postgresTwoTableDDL,
		"1_init.down.sql": "DROP TABLE invoice;\n",
	})

	p, err := Prove(root, rel)
	if err != nil {
		t.Fatalf("Prove: %v", err)
	}
	if p.Ordered {
		t.Errorf("golang-migrate filenames are not integer-versioned; Ordered = true")
	}
	if p.SQLFiles != 2 {
		t.Errorf("SQLFiles = %d, want 2", p.SQLFiles)
	}
	if p.Tables != 0 {
		t.Errorf("Tables = %d, want 0 — an unordered directory must not be parsed at all", p.Tables)
	}
}

// TestProve_MySQLFlavouredDDLDoesNotReconstruct is the M1 MEASUREMENT, kept as a
// permanent control.
//
// codefit does not sniff the SQL dialect (roadmap P0-11), so a directory with no
// `database.type` configured is parsed under the default binding. The open
// question this answers is what happens to a MySQL-flavoured migration set:
// does it reconstruct a PARTLY WRONG model and go live, or does it reconstruct
// nothing and stay commented?
//
// MEASURED: it reconstructs ZERO tables. Both CREATE TABLE statements land in
// the parser's unreducible bucket. The proof gate requires >= 1 table, so a
// MySQL-flavoured directory FAILS the proof and receives the commented block —
// the correct outcome, reached without codefit ever guessing a dialect.
//
// That is what this control locks: not that the parser is right about MySQL, but
// that a dialect which does not reconstruct NEVER GOES LIVE. If a future parser
// change makes this DDL reduce, this test goes red and the residual risk has to
// be re-argued rather than silently acquired.
func TestProve_MySQLFlavouredDDLDoesNotReconstruct(t *testing.T) {
	root := tempRoot(t)
	rel := writeDDL(t, root, "db/migrations", map[string]string{
		"V1__init.sql": mysqlFlavouredDDL,
	})

	p, err := Prove(root, rel)
	if err != nil {
		t.Fatalf("Prove: %v", err)
	}
	if !p.Ordered {
		t.Fatalf("the fixture must reach the parser to measure anything; Ordered = false")
	}
	if p.Tables != 0 {
		t.Errorf("Tables = %d, want 0.\n"+
			"MySQL-flavoured DDL now reconstructs under the default (PostgreSQL) binding. "+
			"That re-opens the dialect residual this control closed: a live schema_paths could "+
			"sit over a partly-wrong model. Do not just update the number — decide whether the "+
			"proof gate still may promote a directory whose dialect codefit never measured.", p.Tables)
	}
}

// TestProve_MissingDirectoryIsAnError keeps a misconfiguration distinct from
// "this project has no schema", exactly as the scan-time resolver does.
func TestProve_MissingDirectoryIsAnError(t *testing.T) {
	root := tempRoot(t)
	if _, err := Prove(root, "db/does-not-exist"); err == nil {
		t.Errorf("Prove over a path that does not exist must error, not report an empty proof")
	}
}
