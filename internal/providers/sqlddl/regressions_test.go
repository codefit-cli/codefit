package sqlddl_test

import (
	"testing"

	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// --- Unit I rework: FIX 1 (C1) — DELIMITER-as-column-name must not corrupt
// the tokenizer. A DELIMITER directive is only recognized when its argument
// is punctuation-only (a real client-tool delimiter token like "//" or "$$"),
// never when it starts with a word character (a type/identifier), so an
// ordinary "delimiter VARCHAR(1)" column definition is never misread as a
// MySQL client directive. Dialect-free: exercised here under Postgres() to
// prove the guard is not MySQL-specific. ---

func TestDelimiterAsColumnName_NotMisreadAsDirective(t *testing.T) {
	// The column starts at the beginning of a line — the exact position
	// matchDelimiterDirective checks (isAtLineStart) — so this reproduces the
	// bug: "delimiter VARCHAR(1)" at line-start was misread as a MySQL client
	// DELIMITER directive because its argument starts with a word character.
	src := "CREATE TABLE csv_configs (\n" +
		"id INT,\n" +
		"delimiter VARCHAR(1),\n" +
		"quote_char VARCHAR(1)\n" +
		");"
	srcs := []providers.SourceFile{{Path: "V1__m.sql", Content: []byte(src)}}
	s, err := sqlddl.New(sqlddl.WithDialect(sqlddl.Postgres())).ParseSchema(srcs)
	if err != nil {
		t.Fatalf("ParseSchema must not error: %v", err)
	}
	tb := table(t, s, "csv_configs")
	if !eqStr(colNames(tb), []string{"id", "delimiter", "quote_char"}) {
		t.Errorf("columns = %v, want [id delimiter quote_char] (DELIMITER-as-column must not corrupt tokenizing)", colNames(tb))
	}
}

// --- FIX 2 (C2 + MINOR) — KEY/INDEX routing must only claim the secondary-
// index FORM (a parenthesized column list), never a bare unquoted column
// literally named key/index. ---

func TestUnquotedKeyColumn_StaysAColumn(t *testing.T) {
	src := "CREATE TABLE t (id int, key text);"
	srcs := []providers.SourceFile{{Path: "V1__m.sql", Content: []byte(src)}}
	s, err := sqlddl.New(sqlddl.WithDialect(sqlddl.Postgres())).ParseSchema(srcs)
	if err != nil {
		t.Fatalf("ParseSchema must not error: %v", err)
	}
	tb := table(t, s, "t")
	if !hasCol(tb, "key") {
		t.Errorf("column 'key' must be present, got columns = %v", colNames(tb))
	}
}

func TestInlineKeyIndexForm_NotAColumn_MySQL(t *testing.T) {
	src := "CREATE TABLE t (id int, KEY idx (id));"
	srcs := []providers.SourceFile{{Path: "V1__m.sql", Content: []byte(src)}}
	s, err := sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())).ParseSchema(srcs)
	if err != nil {
		t.Fatalf("ParseSchema must not error: %v", err)
	}
	tb := table(t, s, "t")
	if hasCol(tb, "key") || hasCol(tb, "KEY") {
		t.Errorf("KEY idx (id) must not become a phantom column, columns = %v", colNames(tb))
	}
	if len(tb.Indexes) != 1 {
		t.Errorf("KEY idx (id) must be recorded as an index, got %+v", tb.Indexes)
	}
}

func TestAlterAddKey_NoPhantomColumn_MySQL(t *testing.T) {
	src := "CREATE TABLE t (id int); ALTER TABLE t ADD KEY idx (col);"
	srcs := []providers.SourceFile{{Path: "V1__m.sql", Content: []byte(src)}}
	s, err := sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())).ParseSchema(srcs)
	if err != nil {
		t.Fatalf("ParseSchema must not error: %v", err)
	}
	tb := table(t, s, "t")
	if hasCol(tb, "KEY") || hasCol(tb, "key") {
		t.Errorf("ADD KEY idx (col) must not add a phantom column named KEY/key, columns = %v", colNames(tb))
	}
}

// --- FIX 3 (M) — a well-formed MySQL DELIMITER // ... DELIMITER ; block must
// not drop statements that follow it, and the reset restores ';' as the
// terminator. ---

func TestDelimiterBlock_WellFormed_SubsequentTablesCaptured(t *testing.T) {
	src := "DELIMITER //\n" +
		"CREATE PROCEDURE p() BEGIN SELECT 1; END//\n" +
		"DELIMITER ;\n" +
		"CREATE TABLE foo(id int);\n" +
		"CREATE TABLE bar(id int);\n"
	srcs := []providers.SourceFile{{Path: "V1__m.sql", Content: []byte(src)}}
	s, err := sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())).ParseSchema(srcs)
	if err != nil {
		t.Fatalf("ParseSchema must not error: %v", err)
	}
	names := tableNames(s)
	if !containsName(names, "foo") || !containsName(names, "bar") {
		t.Errorf("both foo and bar must be captured after the well-formed DELIMITER block; tables = %v", names)
	}
}

// --- FIX 4 (Unit I rework re-judge, CRITICAL) — the KEY/INDEX inline-index
// discriminator must key off the TYPE (via the dialect's TypeMap), not off
// mere '(' presence: a column legitimately named key/index/fulltext/spatial
// whose TYPE carries parens (varchar(255), int(11), numeric(10,2),
// enum('a','b')) must stay a column, never be misread as the inline-index
// FORM and dropped. ---

func TestKeyColumnWithParenType_StaysAColumn_Postgres(t *testing.T) {
	src := "CREATE TABLE t (id int, key varchar(255));"
	srcs := []providers.SourceFile{{Path: "V1__m.sql", Content: []byte(src)}}
	s, err := sqlddl.New(sqlddl.WithDialect(sqlddl.Postgres())).ParseSchema(srcs)
	if err != nil {
		t.Fatalf("ParseSchema must not error: %v", err)
	}
	tb := table(t, s, "t")
	if !hasCol(tb, "key") {
		t.Errorf("column 'key' must be present, got columns = %v", colNames(tb))
	}
	if len(tb.Indexes) != 0 {
		t.Errorf("must not fabricate a phantom index, got %+v", tb.Indexes)
	}
}

func TestKeyColumnWithParenType_StaysAColumn_MySQL(t *testing.T) {
	src := "CREATE TABLE t (id int, key varchar(255));"
	srcs := []providers.SourceFile{{Path: "V1__m.sql", Content: []byte(src)}}
	s, err := sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())).ParseSchema(srcs)
	if err != nil {
		t.Fatalf("ParseSchema must not error: %v", err)
	}
	tb := table(t, s, "t")
	if !hasCol(tb, "key") {
		t.Errorf("column 'key' must be present, got columns = %v", colNames(tb))
	}
	if len(tb.Indexes) != 0 {
		t.Errorf("must not fabricate a phantom index, got %+v", tb.Indexes)
	}
}

func TestIndexColumnWithParenType_StaysAColumn_MySQL(t *testing.T) {
	src := "CREATE TABLE t (id int, index int(11));"
	srcs := []providers.SourceFile{{Path: "V1__m.sql", Content: []byte(src)}}
	s, err := sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())).ParseSchema(srcs)
	if err != nil {
		t.Fatalf("ParseSchema must not error: %v", err)
	}
	tb := table(t, s, "t")
	if !hasCol(tb, "index") {
		t.Errorf("column 'index' must be present, got columns = %v", colNames(tb))
	}
	if len(tb.Indexes) != 0 {
		t.Errorf("must not fabricate a phantom index, got %+v", tb.Indexes)
	}
}

func TestKeyColumnWithNumericParenType_StaysAColumn_MySQL(t *testing.T) {
	src := "CREATE TABLE t (id int, key numeric(10,2));"
	srcs := []providers.SourceFile{{Path: "V1__m.sql", Content: []byte(src)}}
	s, err := sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())).ParseSchema(srcs)
	if err != nil {
		t.Fatalf("ParseSchema must not error: %v", err)
	}
	tb := table(t, s, "t")
	if !hasCol(tb, "key") {
		t.Errorf("column 'key' must be present, got columns = %v", colNames(tb))
	}
	if len(tb.Indexes) != 0 {
		t.Errorf("must not fabricate a phantom index, got %+v", tb.Indexes)
	}
}

func TestKeyColumnWithEnumType_StaysAColumn_MySQL(t *testing.T) {
	// enum('a','b') has parens AND quoted strings — proves the discriminator is
	// type-map-driven, not a digit/paren heuristic.
	src := "CREATE TABLE t (id int, key enum('a','b'));"
	srcs := []providers.SourceFile{{Path: "V1__m.sql", Content: []byte(src)}}
	s, err := sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())).ParseSchema(srcs)
	if err != nil {
		t.Fatalf("ParseSchema must not error: %v", err)
	}
	tb := table(t, s, "t")
	if !hasCol(tb, "key") {
		t.Errorf("column 'key' must be present, got columns = %v", colNames(tb))
	}
	if len(tb.Indexes) != 0 {
		t.Errorf("must not fabricate a phantom index, got %+v", tb.Indexes)
	}
}

func TestSpatialColumnWithParenType_StaysAColumn_MySQL(t *testing.T) {
	// "geometry" is not in the dialect TypeMap (declared subset), so use a
	// type that IS mapped to still exercise the SPATIAL leading keyword.
	src := "CREATE TABLE t (id int, spatial varchar(255));"
	srcs := []providers.SourceFile{{Path: "V1__m.sql", Content: []byte(src)}}
	s, err := sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())).ParseSchema(srcs)
	if err != nil {
		t.Fatalf("ParseSchema must not error: %v", err)
	}
	tb := table(t, s, "t")
	if !hasCol(tb, "spatial") {
		t.Errorf("column 'spatial' must be present, got columns = %v", colNames(tb))
	}
	if len(tb.Indexes) != 0 {
		t.Errorf("must not fabricate a phantom index, got %+v", tb.Indexes)
	}
}

func TestAlterAddKeyColumnWithParenType_StaysAColumn_MySQL(t *testing.T) {
	src := "CREATE TABLE t (id int); ALTER TABLE t ADD key varchar(255);"
	srcs := []providers.SourceFile{{Path: "V1__m.sql", Content: []byte(src)}}
	s, err := sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())).ParseSchema(srcs)
	if err != nil {
		t.Fatalf("ParseSchema must not error: %v", err)
	}
	tb := table(t, s, "t")
	if !hasCol(tb, "key") {
		t.Errorf("column 'key' must be present, got columns = %v", colNames(tb))
	}
	if len(tb.Indexes) != 0 {
		t.Errorf("must not fabricate a phantom index, got %+v", tb.Indexes)
	}
}

func TestAlterAddColumnKeyWithParenType_StillWorks_MySQL(t *testing.T) {
	// with the explicit COLUMN keyword, must keep working as before.
	src := "CREATE TABLE t (id int); ALTER TABLE t ADD COLUMN key varchar(255);"
	srcs := []providers.SourceFile{{Path: "V1__m.sql", Content: []byte(src)}}
	s, err := sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())).ParseSchema(srcs)
	if err != nil {
		t.Fatalf("ParseSchema must not error: %v", err)
	}
	tb := table(t, s, "t")
	if !hasCol(tb, "key") {
		t.Errorf("column 'key' must be present, got columns = %v", colNames(tb))
	}
}

// --- Lock: must NOT over-correct — the real inline-index FORM stays an index,
// never becomes a phantom column, regardless of the fix above. ---

func TestKeyIndexNoName_StillAnIndex_MySQL(t *testing.T) {
	src := "CREATE TABLE t (id int, KEY (id));"
	srcs := []providers.SourceFile{{Path: "V1__m.sql", Content: []byte(src)}}
	s, err := sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())).ParseSchema(srcs)
	if err != nil {
		t.Fatalf("ParseSchema must not error: %v", err)
	}
	tb := table(t, s, "t")
	if hasCol(tb, "key") || hasCol(tb, "KEY") {
		t.Errorf("KEY (id) must not become a phantom column, columns = %v", colNames(tb))
	}
	if len(tb.Indexes) != 1 {
		t.Errorf("KEY (id) must be recorded as an index, got %+v", tb.Indexes)
	}
}

func TestAlterAddKeyIndexForm_StillAnIndex_MySQL(t *testing.T) {
	src := "CREATE TABLE t (id int, col int); ALTER TABLE t ADD KEY idx (col);"
	srcs := []providers.SourceFile{{Path: "V1__m.sql", Content: []byte(src)}}
	s, err := sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())).ParseSchema(srcs)
	if err != nil {
		t.Fatalf("ParseSchema must not error: %v", err)
	}
	tb := table(t, s, "t")
	if hasCol(tb, "key") || hasCol(tb, "KEY") {
		t.Errorf("ADD KEY idx (col) must not add a phantom column, columns = %v", colNames(tb))
	}
	if len(tb.Indexes) != 1 {
		t.Errorf("ADD KEY idx (col) must be recorded as an index, got %+v", tb.Indexes)
	}
}

// --- REMOVE inRoutineBody guard: cross-file/cross-dialect hazards it caused.

// C5: the guard must never leak state across files — a stuck-open guard from
// file1's unterminated routine body must not swallow file2's real table.
func TestRoutineBodyGuardRemoval_NoCrossFileLeak(t *testing.T) {
	file1 := "CREATE PROCEDURE p AS BEGIN SELECT 1;"
	file2 := "CREATE TABLE real_table (id int);"
	srcs := []providers.SourceFile{
		{Path: "V1__p.sql", Content: []byte(file1)},
		{Path: "V2__t.sql", Content: []byte(file2)},
	}
	s, err := sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer())).ParseSchema(srcs)
	if err != nil {
		t.Fatalf("ParseSchema must not error: %v", err)
	}
	if !containsName(tableNames(s), "real_table") {
		t.Errorf("real_table must be captured regardless of file1's unterminated routine body; tables = %v", tableNames(s))
	}
}

// C3: nested BEGIN...END inside a routine body must not crash (depth is not
// tracked; this is a documented limit, not a guard responsibility anymore).
func TestRoutineBody_NestedBeginEnd_NoCrash(t *testing.T) {
	src := "CREATE PROCEDURE p AS\n" +
		"BEGIN\n" +
		"  IF 1=1 BEGIN SELECT 1; END\n" +
		"  SELECT 2;\n" +
		"END\n" +
		"GO\n" +
		"CREATE TABLE after_nested (id int);\n"
	srcs := []providers.SourceFile{{Path: "V1__m.sql", Content: []byte(src)}}
	if _, err := sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer())).ParseSchema(srcs); err != nil {
		t.Fatalf("ParseSchema must not error (no crash) on nested BEGIN/END: %v", err)
	}
}

// C4: a BEGIN/END-shaped word appearing inside a string literal in a routine
// body must not crash (documented limit — bodies are matched as raw text).
func TestRoutineBody_EndInStringLiteral_NoCrash(t *testing.T) {
	src := "CREATE PROCEDURE p AS\n" +
		"BEGIN\n" +
		"  PRINT 'Reached END state';\n" +
		"  SELECT 1;\n" +
		"END\n" +
		"GO\n" +
		"CREATE TABLE after_string (id int);\n"
	srcs := []providers.SourceFile{{Path: "V1__m.sql", Content: []byte(src)}}
	if _, err := sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer())).ParseSchema(srcs); err != nil {
		t.Fatalf("ParseSchema must not error (no crash) on END-in-string: %v", err)
	}
}
