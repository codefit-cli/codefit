package sqlddl_test

import (
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// --- I2: MySQL DELIMITER-changed trigger/proc body must not leak a phantom table ---

func TestPhantomGuard_MySQLDelimiterBody_NoPhantomTable(t *testing.T) {
	src := "CREATE TABLE orders (id INT, status VARCHAR(10));\n" +
		"DELIMITER //\n" +
		"CREATE TRIGGER trg_orders_audit BEFORE INSERT ON orders FOR EACH ROW\n" +
		"BEGIN\n" +
		"  INSERT INTO audit_log (msg) VALUES ('insert');\n" +
		"  CREATE TABLE evil_leak (id INT);\n" +
		"END//\n" +
		"DELIMITER ;\n" +
		"CREATE TABLE customers (id INT PRIMARY KEY);\n"

	srcs := []providers.SourceFile{{Path: "V1__m.sql", Content: []byte(src)}}
	s, err := sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())).ParseSchema(srcs)
	if err != nil {
		t.Fatalf("ParseSchema must not error (no crash): %v", err)
	}

	names := tableNames(s)
	for _, n := range names {
		if n == "evil_leak" || n == "audit_log" {
			t.Errorf("phantom table %q leaked from the DELIMITER-protected trigger body; tables = %v", n, names)
		}
	}
	if !containsName(names, "orders") || !containsName(names, "customers") {
		t.Errorf("real tables before/after the DELIMITER body must still be captured; got %v", names)
	}
	if len(s.Triggers) != 1 {
		t.Fatalf("trigger HEAD must still be captured despite the body being out of scope, got %d triggers", len(s.Triggers))
	}
	if s.Triggers[0].Name != "trg_orders_audit" || s.Triggers[0].Table != "orders" {
		t.Errorf("trigger = %+v, want Name=trg_orders_audit Table=orders", s.Triggers[0])
	}
}

// --- I3 (Unit I rework, CLOSED by ADR 0027): T-SQL GO-separated batches —
// real CREATE TABLEs on both sides of a GO-batched proc body must still be
// captured, and parsing must never crash. The T-SQL routine-body
// phantom-table guard (inRoutineBody) was REMOVED (Unit I rework, C3/C4/C5):
// it matched BEGIN/END as raw text (also inside string literals), was not
// depth-counted (a nested BEGIN...END closed it early), and leaked state
// across files (a stuck-open guard from one file could swallow a LATER
// file's real tables). ADR 0027's split()-level head-awareness closes the
// documented limit from a different angle, without resurrecting a BEGIN/END
// counter: once split() recognizes a CREATE FUNCTION/PROCEDURE/TRIGGER head,
// it suspends ';'-flushing for the whole body up to GO/EOF, so an in-body
// CREATE TABLE-shaped fragment is absorbed as TEXT inside the routine's Body
// rather than ever being tokenized as its own top-level statement — EvilLeak
// no longer leaks as a phantom table. ---

func TestTSQLGoBatches_RoutineBodyAbsorbsInBodyCreateTable(t *testing.T) {
	src := "CREATE TABLE [dbo].[Orders] ([Id] INT PRIMARY KEY);\n" +
		"GO\n" +
		"CREATE PROCEDURE dbo.AuditOrder\n" +
		"AS\n" +
		"BEGIN\n" +
		"  INSERT INTO AuditLog (Msg) VALUES ('x');\n" +
		"  CREATE TABLE EvilLeak (Id INT);\n" +
		"END\n" +
		"GO\n" +
		"CREATE TABLE [dbo].[Customers] ([Id] INT PRIMARY KEY);\n" +
		"GO\n"

	srcs := []providers.SourceFile{{Path: "V1__m.sql", Content: []byte(src)}}
	s, err := sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer())).ParseSchema(srcs)
	if err != nil {
		t.Fatalf("ParseSchema must not error (no crash): %v", err)
	}

	names := tableNames(s)
	if !containsName(names, "Orders") || !containsName(names, "Customers") {
		t.Errorf("real tables before/after the GO-batched proc must still be captured; got %v", names)
	}
	if len(s.Procedures) != 1 || s.Procedures[0].Name != "AuditOrder" {
		t.Errorf("procedure HEAD must still be captured, got %+v", s.Procedures)
	}
	// CLOSED: EvilLeak must NOT surface as a spurious top-level table anymore
	// — it is absorbed into the procedure's body text instead.
	if containsName(names, "EvilLeak") {
		t.Errorf("EvilLeak must NOT be a captured top-level table (it must be absorbed into the proc body instead); tables = %v", names)
	}
	if len(s.Procedures) == 1 {
		body := s.Procedures[0].Body
		if !body.Complete {
			t.Errorf("Body.Complete = false, want true (full capture to GO); Note=%q Text=%q", body.Note, body.Text)
		}
		if !strings.Contains(body.Text, "EvilLeak") {
			t.Errorf("Body.Text = %q, want it to contain the absorbed CREATE TABLE EvilLeak fragment (proof it was absorbed, not dropped)", body.Text)
		}
	}
}

func containsName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// --- I4b: MySQL inline KEY/INDEX secondary-index shorthand must not become a
// phantom column, and must not drop sibling columns ---

func TestInlineKeyShorthand_NotAPhantomColumn(t *testing.T) {
	src := "CREATE TABLE `film_actor` (\n" +
		"  `actor_id` SMALLINT UNSIGNED NOT NULL,\n" +
		"  `film_id` SMALLINT UNSIGNED NOT NULL,\n" +
		"  PRIMARY KEY (`actor_id`, `film_id`),\n" +
		"  KEY `idx_fk_film_id` (`film_id`)\n" +
		");"
	srcs := []providers.SourceFile{{Path: "V1__m.sql", Content: []byte(src)}}
	s, err := sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())).ParseSchema(srcs)
	if err != nil {
		t.Fatalf("ParseSchema must not error: %v", err)
	}
	tb := table(t, s, "film_actor")
	if hasCol(tb, "KEY") || hasCol(tb, "key") {
		t.Errorf("inline KEY shorthand must not become a phantom column, columns = %v", colNames(tb))
	}
	if !eqStr(colNames(tb), []string{"actor_id", "film_id"}) {
		t.Errorf("sibling columns must survive, got %v", colNames(tb))
	}
}

func TestInlineIndexShorthand_NotAPhantomColumn(t *testing.T) {
	src := "CREATE TABLE `t` (\n" +
		"  `a` INT,\n" +
		"  `b` INT,\n" +
		"  INDEX `idx_b` (`b`)\n" +
		");"
	srcs := []providers.SourceFile{{Path: "V1__m.sql", Content: []byte(src)}}
	s, err := sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())).ParseSchema(srcs)
	if err != nil {
		t.Fatalf("ParseSchema must not error: %v", err)
	}
	tb := table(t, s, "t")
	if hasCol(tb, "INDEX") || hasCol(tb, "index") {
		t.Errorf("inline INDEX shorthand must not become a phantom column, columns = %v", colNames(tb))
	}
	if !eqStr(colNames(tb), []string{"a", "b"}) {
		t.Errorf("sibling columns must survive, got %v", colNames(tb))
	}
}

// --- I5: a trailing PARTITION BY clause must not prevent the table + its
// real columns from being captured, and must not crash. (The clause itself
// is no longer ignored — partition-capture reads it into
// db.Table.Partitioning; what this test locks is that reading it leaves the
// BODY untouched. See partition_capture_test.go for the clause itself.) ---

func TestPartitionClause_TableAndColumnsCaptured(t *testing.T) {
	src := `CREATE TABLE sales (
  id int,
  sold_at date,
  region varchar(10)
) PARTITION BY RANGE (id) (
  PARTITION p0 VALUES LESS THAN (1000),
  PARTITION p1 VALUES LESS THAN (MAXVALUE)
);`
	s := parse(t, src)
	tb := table(t, s, "sales")
	if !eqStr(colNames(tb), []string{"id", "sold_at", "region"}) {
		t.Errorf("columns = %v, want [id sold_at region] (the trailing partition clause is read into Partitioning, and must not disturb the body)", colNames(tb))
	}
}

// --- I6: computed/generated columns must be captured (best-effort type or
// TypeUnknown) or skipped, but never crash and never drop sibling columns ---

func TestComputedColumn_MySQLGeneratedAlwaysAs_NoCrashNoDrop(t *testing.T) {
	src := "CREATE TABLE `people` (\n" +
		"  `first` VARCHAR(50),\n" +
		"  `last` VARCHAR(50),\n" +
		"  `full_name` VARCHAR(101) GENERATED ALWAYS AS (CONCAT(`first`,' ',`last`)) STORED,\n" +
		"  `age` INT\n" +
		");"
	srcs := []providers.SourceFile{{Path: "V1__m.sql", Content: []byte(src)}}
	s, err := sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())).ParseSchema(srcs)
	if err != nil {
		t.Fatalf("ParseSchema must not error: %v", err)
	}
	tb := table(t, s, "people")
	if !eqStr(colNames(tb), []string{"first", "last", "full_name", "age"}) {
		t.Errorf("columns = %v, want all 4 columns present (generated column captured, siblings untouched)", colNames(tb))
	}
}

func TestComputedColumn_TSQLComputedColumn_NoCrashNoDrop(t *testing.T) {
	src := "CREATE TABLE [dbo].[People] (\n" +
		"  [First] varchar(50),\n" +
		"  [Last] varchar(50),\n" +
		"  [Full] AS ([First]+' '+[Last]),\n" +
		"  [Age] int\n" +
		");"
	srcs := []providers.SourceFile{{Path: "V1__m.sql", Content: []byte(src)}}
	s, err := sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer())).ParseSchema(srcs)
	if err != nil {
		t.Fatalf("ParseSchema must not error: %v", err)
	}
	tb := table(t, s, "People")
	if !eqStr(colNames(tb), []string{"First", "Last", "Full", "Age"}) {
		t.Errorf("columns = %v, want all 4 columns present (computed column captured, siblings untouched)", colNames(tb))
	}
}

// --- I7: T-SQL CREATE TYPE must not crash and must not corrupt subsequent
// statements ---

func TestCreateType_TSQL_SkippedNoCrash(t *testing.T) {
	src := "CREATE TYPE dbo.Money2 FROM DECIMAL(19,4) NOT NULL;\n" +
		"CREATE TABLE [dbo].[Prices] ([Id] INT PRIMARY KEY, [Amount] dbo.Money2 NOT NULL);\n"
	srcs := []providers.SourceFile{{Path: "V1__m.sql", Content: []byte(src)}}
	s, err := sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer())).ParseSchema(srcs)
	if err != nil {
		t.Fatalf("ParseSchema must not error: %v", err)
	}
	names := tableNames(s)
	if !containsName(names, "Prices") {
		t.Errorf("the table AFTER an unsupported CREATE TYPE must still parse; tables = %v", names)
	}
}

// --- I9: one-dialect-per-project — PostgreSQL syntax parsed under the mysql
// dialect must be a documented best-effort limit, never a crash, never
// silently claimed as fully correct ---

func TestOneDialectPerProject_PostgresSyntaxUnderMySQLDialect(t *testing.T) {
	// SERIAL and a $$-delimited function body are PostgreSQL-only constructs.
	// Under the MySQL dialect (DollarQuoting=false), the $$ markers are NOT
	// recognized as a quoting mechanism, so the function body's internal ';'
	// DOES cut the statement early — a real, observed degradation, not a
	// crash, and not silently presented as correct PL/pgSQL support.
	src := `CREATE TABLE t (id SERIAL PRIMARY KEY, name TEXT);
CREATE FUNCTION f() RETURNS int AS $$ BEGIN RETURN 1; END; $$ LANGUAGE plpgsql;
CREATE TABLE u (id INT PRIMARY KEY);`
	srcs := []providers.SourceFile{{Path: "V1__m.sql", Content: []byte(src)}}
	s, err := sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())).ParseSchema(srcs)
	if err != nil {
		t.Fatalf("ParseSchema must not error even for wrong-dialect input (no crash): %v", err)
	}
	tb := table(t, s, "t")
	// SERIAL is not in the MySQL TypeMap -> honest TypeUnknown, not a crash,
	// not silently mapped to db.TypeInt as if it understood PG's SERIAL.
	for _, c := range tb.Columns {
		if c.Name == "id" && c.Type == db.TypeInt {
			t.Errorf("SERIAL under the mysql dialect must NOT silently resolve to Int (that would claim understanding it doesn't have); got %s", c.Type)
		}
	}
	// The table declared AFTER the dollar-quoted body must still parse: this
	// is the documented limit — the mismatched dialect degrades gracefully
	// rather than corrupting unrelated, later statements.
	if !containsName(tableNames(s), "u") {
		t.Errorf("table 'u' (after the wrong-dialect function body) must still be captured; tables = %v", tableNames(s))
	}
}
