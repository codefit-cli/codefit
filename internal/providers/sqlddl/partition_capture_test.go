package sqlddl_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/sqlddl"
)

// partition-capture: the SQL-DDL reducer READS table partitioning into the
// neutral model (db.Table.Partitioning) instead of ignoring it.
//
// Every assertion below is transcribed FROM THE SOURCE DDL in the test (or,
// for the vendored-corpus cases, from the DDL text read out of testdata/) —
// never from what the parser happened to return. GROUND TRUTH before this
// slice, measured with a throwaway probe over the real parser:
//
//	PG    "CREATE TABLE t (...) PARTITION BY RANGE (c)"  -> tail IGNORED, Complete=true
//	PG    "CREATE TABLE c PARTITION OF p FOR VALUES ..." -> the WHOLE TABLE VANISHED
//	                                                        (tables=0, nothing recorded)
//	MySQL "CREATE TABLE t (...) PARTITION BY RANGE (..)" -> tail IGNORED, Complete=true
//	T-SQL "CREATE TABLE t (...) ON [Scheme]([col])"      -> tail IGNORED, Complete=true
//	T-SQL "CREATE PARTITION FUNCTION/SCHEME ..."         -> silently dropped
//	PG    "ALTER TABLE p ATTACH PARTITION c ..."         -> already at the honest-
//	                                                        abstention floor (unchanged)

// parsePart parses one source string with p and returns the schema.
func parsePart(t *testing.T, p *sqlddl.Parser, src string) *db.Schema {
	t.Helper()
	s, err := p.ParseSchema([]providers.SourceFile{{Path: "x.sql", Content: []byte(src)}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	return s
}

// partTable returns the named table or fails, naming what WAS parsed.
func partTable(t *testing.T, s *db.Schema, name string) db.Table {
	t.Helper()
	var names []string
	for _, tb := range s.Tables {
		if tb.Name == name {
			return tb
		}
		names = append(names, tb.Name)
	}
	t.Fatalf("table %q not found; parsed tables = %v", name, names)
	return db.Table{}
}

// ---------------------------------------------------------------------------
// PostgreSQL: the parent's PARTITION BY <strategy> (<key>) clause
// ---------------------------------------------------------------------------

// Source DDL: PARTITION BY RANGE (logdate). Transcribed from the source: the
// strategy word is RANGE, the key is the single column logdate.
func TestSQLDDL_PG_PartitionByRange_CapturesStrategyAndKey(t *testing.T) {
	s := parsePart(t, sqlddl.New(), `
CREATE TABLE measurement (
    city_id int not null,
    logdate date not null,
    peaktemp int
) PARTITION BY RANGE (logdate);
`)
	tb := partTable(t, s, "measurement")
	if got := tb.Partitioning.Strategy; got != "range" {
		t.Errorf("Partitioning.Strategy = %q, want %q — the source says PARTITION BY RANGE", got, "range")
	}
	if got := tb.Partitioning.Key; len(got) != 1 || got[0] != "logdate" {
		t.Errorf("Partitioning.Key = %v, want [logdate] — the source says RANGE (logdate)", got)
	}
	if tb.Partitioning.Declaration == "" {
		t.Error("Partitioning.Declaration is empty — a table whose source declares partitioning must carry the clause verbatim")
	}
	if tb.Partitioning.Of != "" {
		t.Errorf("Partitioning.Of = %q, want empty — this table is the PARENT, not a child", tb.Partitioning.Of)
	}
}

// The parent's own columns/keys are ALL declared in the statement: reading the
// tail must ADD information, never demote the table. A regression here mutes
// every absence-based DB rule across ordinary partitioned DDL.
func TestSQLDDL_PG_PartitionedParent_StaysComplete(t *testing.T) {
	s := parsePart(t, sqlddl.New(), `
CREATE TABLE measurement (
    city_id int not null,
    logdate date not null,
    peaktemp int
) PARTITION BY RANGE (logdate);
`)
	tb := partTable(t, s, "measurement")
	if !tb.Complete {
		t.Errorf("Complete = false (Note %q), want true — every column of this table is declared in the statement; partitioning is read, not a drop", tb.Note)
	}
	if len(tb.Columns) != 3 {
		t.Errorf("columns = %d, want 3 (city_id, logdate, peaktemp) — reading the tail must not disturb the body", len(tb.Columns))
	}
}

// Source DDL declares LIST and HASH. Both strategy words are captured
// lowercased, verbatim from the source.
func TestSQLDDL_PG_PartitionByListAndHash_CapturesStrategy(t *testing.T) {
	s := parsePart(t, sqlddl.New(), `
CREATE TABLE t_list (a int, b text) PARTITION BY LIST (b);
CREATE TABLE t_hash (a int, b text) PARTITION BY HASH (a);
`)
	if got := partTable(t, s, "t_list").Partitioning.Strategy; got != "list" {
		t.Errorf("t_list Strategy = %q, want %q — the source says PARTITION BY LIST", got, "list")
	}
	if got := partTable(t, s, "t_hash").Partitioning.Strategy; got != "hash" {
		t.Errorf("t_hash Strategy = %q, want %q — the source says PARTITION BY HASH", got, "hash")
	}
}

// Source DDL: PARTITION BY RANGE (tenant_id, logdate) — a COMPOSITE key. Both
// columns, in source order.
func TestSQLDDL_PG_PartitionByCompositeKey_CapturesBothColumns(t *testing.T) {
	s := parsePart(t, sqlddl.New(), `
CREATE TABLE m (tenant_id int, logdate date) PARTITION BY RANGE (tenant_id, logdate);
`)
	got := partTable(t, s, "m").Partitioning.Key
	if len(got) != 2 || got[0] != "tenant_id" || got[1] != "logdate" {
		t.Errorf("Partitioning.Key = %v, want [tenant_id logdate] — the source says RANGE (tenant_id, logdate)", got)
	}
}

// ---------------------------------------------------------------------------
// PostgreSQL: the CHILD, "CREATE TABLE c PARTITION OF p FOR VALUES ..."
// ---------------------------------------------------------------------------

// Before this slice the child table VANISHED ENTIRELY (measured: tables=0).
// It must now exist, name its parent, and be UNPROVEN: its columns and keys
// are inherited from the parent and are declared NOWHERE in this statement,
// so any rule concluding from their absence must abstain.
func TestSQLDDL_PG_PartitionChild_IsRegisteredNamesParentAndIsUnproven(t *testing.T) {
	s := parsePart(t, sqlddl.New(), `
CREATE TABLE measurement (city_id int, logdate date) PARTITION BY RANGE (logdate);
CREATE TABLE measurement_y2022 PARTITION OF measurement
    FOR VALUES FROM ('2022-01-01') TO ('2023-01-01');
`)
	tb := partTable(t, s, "measurement_y2022")
	if got := tb.Partitioning.Of; got != "measurement" {
		t.Errorf("Partitioning.Of = %q, want %q — the source says PARTITION OF measurement", got, "measurement")
	}
	if tb.Complete {
		t.Error("Complete = true, want false — this statement declares NO columns and NO keys; they are inherited from the parent and were never read, so an absence-based rule must abstain over this table")
	}
	if len(tb.Unreduced) == 0 {
		t.Error("Unreduced is empty — the honest-abstention floor must keep the user's own statement text")
	}
}

// The DEFAULT partition form carries no FOR VALUES clause at all.
func TestSQLDDL_PG_PartitionChildDefault_IsRegistered(t *testing.T) {
	s := parsePart(t, sqlddl.New(), `
CREATE TABLE measurement_def PARTITION OF measurement DEFAULT;
`)
	tb := partTable(t, s, "measurement_def")
	if got := tb.Partitioning.Of; got != "measurement" {
		t.Errorf("Partitioning.Of = %q, want %q — the source says PARTITION OF measurement DEFAULT", got, "measurement")
	}
}

// A schema-qualified parent must be normalized the same way every other table
// reference in this reducer is (public.measurement -> measurement), or the
// child would point at a parent name no table in the model ever carries.
func TestSQLDDL_PG_PartitionChild_ParentNameIsNormalized(t *testing.T) {
	s := parsePart(t, sqlddl.New(), `
CREATE TABLE public.measurement_y2022 PARTITION OF public.measurement
    FOR VALUES FROM ('2022-01-01') TO ('2023-01-01');
`)
	if got := partTable(t, s, "measurement_y2022").Partitioning.Of; got != "measurement" {
		t.Errorf("Partitioning.Of = %q, want %q — public.measurement normalizes to measurement", got, "measurement")
	}
}

// ---------------------------------------------------------------------------
// MySQL
// ---------------------------------------------------------------------------

// Source DDL: PARTITION BY RANGE (`sold_on`) followed by an explicit
// partition-definition list. Strategy range, key the single column sold_on;
// the body's own PRIMARY KEY must survive untouched.
func TestSQLDDL_MySQL_PartitionByRange_CapturesStrategyAndKey(t *testing.T) {
	s := parsePart(t, sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())), "CREATE TABLE `sales` (\n"+
		"  `id` INT NOT NULL,\n"+
		"  `sold_on` DATE NOT NULL,\n"+
		"  PRIMARY KEY (`id`,`sold_on`)\n"+
		") ENGINE=InnoDB\n"+
		"PARTITION BY RANGE (`sold_on`) (\n"+
		"  PARTITION p0 VALUES LESS THAN (2020),\n"+
		"  PARTITION p1 VALUES LESS THAN MAXVALUE\n"+
		");\n")
	tb := partTable(t, s, "sales")
	if got := tb.Partitioning.Strategy; got != "range" {
		t.Errorf("Strategy = %q, want %q — the source says PARTITION BY RANGE", got, "range")
	}
	if got := tb.Partitioning.Key; len(got) != 1 || got[0] != "sold_on" {
		t.Errorf("Key = %v, want [sold_on] — the source says RANGE (`sold_on`)", got)
	}
	if got := tb.PrimaryKey; len(got) != 2 || got[0] != "id" || got[1] != "sold_on" {
		t.Errorf("PrimaryKey = %v, want [id sold_on] — the body's PRIMARY KEY must be unaffected", got)
	}
	if !tb.Complete {
		t.Errorf("Complete = false (Note %q), want true — every column is declared in the body", tb.Note)
	}
}

// MySQL's KEY strategy takes an explicit column list; PARTITIONS <n> follows.
func TestSQLDDL_MySQL_PartitionByKey_CapturesStrategyAndKey(t *testing.T) {
	s := parsePart(t, sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())),
		"CREATE TABLE `k` (`id` INT NOT NULL, PRIMARY KEY (`id`)) PARTITION BY KEY(`id`) PARTITIONS 4;\n")
	tb := partTable(t, s, "k")
	if got := tb.Partitioning.Strategy; got != "key" {
		t.Errorf("Strategy = %q, want %q — the source says PARTITION BY KEY", got, "key")
	}
	if got := tb.Partitioning.Key; len(got) != 1 || got[0] != "id" {
		t.Errorf("Key = %v, want [id] — the source says KEY(`id`)", got)
	}
}

// FABRICATION GUARD. Source DDL: PARTITION BY RANGE ( YEAR(`sold_on`) ). The
// partition key is an EXPRESSION, not a column list. splitIdents — the
// reducer's ordinary column-list splitter — would return the single "column"
// `YEAR(sold_on`, a column that does not exist in this table or any other.
// The key must therefore stay EMPTY (never guessed) while the declaration is
// still reported, so a consumer can read the source instead of believing an
// invented column name.
func TestSQLDDL_MySQL_ExpressionPartitionKey_IsNotFabricatedIntoAColumn(t *testing.T) {
	s := parsePart(t, sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())), "CREATE TABLE `sales` (\n"+
		"  `id` INT NOT NULL,\n"+
		"  `sold_on` DATE NOT NULL\n"+
		") PARTITION BY RANGE ( YEAR(`sold_on`) ) (\n"+
		"  PARTITION p0 VALUES LESS THAN (2020)\n"+
		");\n")
	tb := partTable(t, s, "sales")
	if len(tb.Partitioning.Key) != 0 {
		t.Errorf("Key = %v, want EMPTY — the source key is the EXPRESSION YEAR(`sold_on`), not a column list; any entry here is fabricated", tb.Partitioning.Key)
	}
	if tb.Partitioning.Declaration == "" {
		t.Error("Declaration is empty — abstaining on the key must not make the whole declaration vanish silently")
	}
	if got := tb.Partitioning.Strategy; got != "range" {
		t.Errorf("Strategy = %q, want %q — the strategy word IS in the source even when the key is an expression", got, "range")
	}
	// Every fabricated key would also have to be a real column of the table:
	// prove the column set was untouched.
	if len(tb.Columns) != 2 {
		t.Errorf("columns = %d, want 2 (id, sold_on)", len(tb.Columns))
	}
}

// ---------------------------------------------------------------------------
// T-SQL
// ---------------------------------------------------------------------------

// Source DDL: ") ON [OrdersScheme]([OrderDate]);". T-SQL declares table
// partitioning at the table only by naming a partition SCHEME plus the
// partitioning column; the strategy itself lives in the partition FUNCTION.
func TestSQLDDL_TSQL_OnPartitionScheme_CapturesSchemeAndKey(t *testing.T) {
	s := parsePart(t, sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer())), `
CREATE TABLE [dbo].[Orders] (
    [OrderID] INT NOT NULL,
    [OrderDate] DATETIME NOT NULL
) ON [OrdersScheme]([OrderDate]);
`)
	tb := partTable(t, s, "Orders")
	if got := tb.Partitioning.Scheme; got != "OrdersScheme" {
		t.Errorf("Scheme = %q, want %q — the source says ON [OrdersScheme](...)", got, "OrdersScheme")
	}
	if got := tb.Partitioning.Key; len(got) != 1 || got[0] != "OrderDate" {
		t.Errorf("Key = %v, want [OrderDate] — the source says [OrdersScheme]([OrderDate])", got)
	}
	if tb.Partitioning.Declaration == "" {
		t.Error("Declaration is empty — the ON <scheme>(<col>) clause must be reported verbatim")
	}
	if !tb.Complete {
		t.Errorf("Complete = false (Note %q), want true — both columns are declared in the body", tb.Note)
	}
}

// The partition FUNCTION spells the strategy (AS RANGE RIGHT). When the
// scheme and its function are in the parsed DDL, the strategy word is read
// from the source, never assumed.
func TestSQLDDL_TSQL_PartitionFunctionStrategy_IsResolvedThroughTheScheme(t *testing.T) {
	s := parsePart(t, sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer())), `
CREATE PARTITION FUNCTION OrdersPF (datetime) AS RANGE RIGHT FOR VALUES ('2022-01-01','2023-01-01');
GO
CREATE PARTITION SCHEME OrdersScheme AS PARTITION OrdersPF ALL TO ([PRIMARY]);
GO
CREATE TABLE [dbo].[Orders] (
    [OrderID] INT NOT NULL,
    [OrderDate] DATETIME NOT NULL
) ON [OrdersScheme]([OrderDate]);
`)
	if got := partTable(t, s, "Orders").Partitioning.Strategy; got != "range" {
		t.Errorf("Strategy = %q, want %q — CREATE PARTITION FUNCTION OrdersPF ... AS RANGE RIGHT says RANGE, and OrdersScheme is built on OrdersPF", got, "range")
	}
}

// When the scheme is NOT in the parsed DDL, the strategy is unknown. It must
// stay EMPTY — never defaulted to "range" just because T-SQL has only one
// partition-function strategy word.
func TestSQLDDL_TSQL_UnresolvableScheme_LeavesStrategyEmpty(t *testing.T) {
	s := parsePart(t, sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer())), `
CREATE TABLE [dbo].[Orders] (
    [OrderID] INT NOT NULL,
    [OrderDate] DATETIME NOT NULL
) ON [OrdersScheme]([OrderDate]);
`)
	tb := partTable(t, s, "Orders")
	if got := tb.Partitioning.Strategy; got != "" {
		t.Errorf("Strategy = %q, want EMPTY — no CREATE PARTITION FUNCTION for OrdersScheme was parsed, so the strategy is not in the source codefit read", got)
	}
	if tb.Partitioning.Scheme == "" {
		t.Error("Scheme is empty — the scheme NAME is in the source and must still be reported")
	}
}

// FABRICATION GUARD, against the REAL vendored corpus. AdventureWorksDW's
// three CREATE TABLE statements all end ") ON [PRIMARY];" — a FILEGROUP
// reference, which is NOT partitioning: T-SQL's grammar admits a
// parenthesized column ONLY for the partition-scheme form. Reading a
// filegroup as a partition scheme would tell an agent that every one of
// these DW tables is partitioned when none is.
func TestSQLDDL_TSQL_OnFilegroup_IsNotReadAsPartitioning(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("testdata", "tsql", "adventureworksdw_real_objects.sql"))
	if err != nil {
		t.Fatal(err)
	}
	p := sqlddl.New(sqlddl.WithDialect(sqlddl.SQLServer()))
	sch, err := p.ParseSchema([]providers.SourceFile{{Path: "adventureworksdw_real_objects.sql", Content: content}})
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	if len(sch.Tables) != 3 {
		t.Fatalf("tables = %d, want 3 (DimCustomer, DimDate, FactInternetSales) — the fixture no longer has the shape this test exercises", len(sch.Tables))
	}
	for _, tb := range sch.Tables {
		if tb.Partitioning.Declaration != "" {
			t.Errorf("table %s: Partitioning.Declaration = %q, want EMPTY — its source ends ') ON [PRIMARY];', a FILEGROUP, not a partition scheme", tb.Name, tb.Partitioning.Declaration)
		}
		if tb.Partitioning.Scheme != "" {
			t.Errorf("table %s: Partitioning.Scheme = %q, want EMPTY — [PRIMARY] is a filegroup", tb.Name, tb.Partitioning.Scheme)
		}
	}
}

// ---------------------------------------------------------------------------
// Cross-cutting guards
// ---------------------------------------------------------------------------

// "PARTITION BY" is also WINDOW FUNCTION syntax (OVER (PARTITION BY ...)),
// which has nothing whatever to do with table partitioning.
//
// This is NOT a hypothetical collision. PostgreSQL's CREATE TABLE ... AS
// SELECT admits a column-name list, so "CREATE TABLE s (a, b) AS SELECT ...
// OVER (PARTITION BY c) ..." is valid DDL that the reducer's ordinary CREATE
// TABLE path DOES dispatch (verified: it yields the table "summary" with 2
// columns), putting a window function's PARTITION BY directly into the tail
// this slice reads. It sits at paren depth 1, inside OVER's parentheses —
// which is exactly why the tail is searched at TOP LEVEL only. A plain regex
// search here would report "summary is partitioned by customer_id".
func TestSQLDDL_WindowFunctionPartitionByInTableTail_IsNotTablePartitioning(t *testing.T) {
	s := parsePart(t, sqlddl.New(), `
CREATE TABLE summary (customer_id, rn) AS
    SELECT customer_id, row_number() OVER (PARTITION BY customer_id ORDER BY rental_id) FROM rental;
`)
	tb := partTable(t, s, "summary")
	if tb.Partitioning.Declaration != "" {
		t.Errorf("Partitioning.Declaration = %q, want EMPTY — the only PARTITION BY in this source belongs to a WINDOW FUNCTION inside OVER (...)", tb.Partitioning.Declaration)
	}
	if len(tb.Partitioning.Key) != 0 {
		t.Errorf("Partitioning.Key = %v, want EMPTY — customer_id is a window PARTITION BY, not a table partition key", tb.Partitioning.Key)
	}
}

// EMPTY means "not declared in source", never a guessed default — the same
// convention db.Index.Method and db.Table.DBName already hold to.
func TestSQLDDL_UnpartitionedTable_DeclaresNoPartitioning(t *testing.T) {
	s := parsePart(t, sqlddl.New(), `
CREATE TABLE plain (id int PRIMARY KEY, name text);
`)
	tb := partTable(t, s, "plain")
	if tb.Partitioning.Declaration != "" || tb.Partitioning.Strategy != "" ||
		tb.Partitioning.Scheme != "" || tb.Partitioning.Of != "" || len(tb.Partitioning.Key) != 0 {
		t.Errorf("Partitioning = %+v, want the zero value — this source declares no partitioning at all", tb.Partitioning)
	}
}

// No vendored corpus declares table partitioning (verified by reading every
// .sql under testdata/: the only PARTITION occurrences are inside COMMENTS).
// If any table in any corpus starts reporting partitioning, the reader is
// firing on ordinary DDL.
func TestSQLDDL_NoVendoredCorpusDeclaresPartitioning(t *testing.T) {
	dialects := map[string]sqlddl.Dialect{
		".":     sqlddl.Postgres(),
		"mysql": sqlddl.MySQL(),
		"tsql":  sqlddl.SQLServer(),
	}
	var seen int
	err := filepath.Walk("testdata", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".sql" {
			return err
		}
		rel, _ := filepath.Rel("testdata", path)
		d, ok := dialects[filepath.Dir(rel)]
		if !ok {
			t.Fatalf("corpus %s lives in an unmapped directory — map its dialect, do not skip it", rel)
		}
		content, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		sch, perr := sqlddl.New(sqlddl.WithDialect(d)).ParseSchema([]providers.SourceFile{{Path: rel, Content: content}})
		if perr != nil {
			t.Fatalf("%s: ParseSchema: %v", rel, perr)
		}
		seen++
		for _, tb := range sch.Tables {
			if tb.Partitioning.Declaration != "" {
				t.Errorf("%s: table %s reports Partitioning.Declaration = %q, but no corpus DDL declares table partitioning — the reader is firing on ordinary DDL", rel, tb.Name, tb.Partitioning.Declaration)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if seen == 0 {
		t.Fatal("walked no corpora — the sweep is broken, so its result means nothing")
	}
}

// A MySQL table COMMENT is an ordinary string literal and may contain any
// text at all, including the words "partition by". A bare regex search over
// the table tail would reduce a comment into a declared partitioning.
func TestSQLDDL_MySQL_PartitionByInsideACommentString_IsNotRead(t *testing.T) {
	s := parsePart(t, sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())),
		"CREATE TABLE `notes` (`id` INT NOT NULL) ENGINE=InnoDB COMMENT='we should partition by range (created_at) one day';\n")
	tb := partTable(t, s, "notes")
	if tb.Partitioning.Declaration != "" {
		t.Errorf("Declaration = %q, want EMPTY — the only 'partition by' in this source is prose inside a COMMENT string literal", tb.Partitioning.Declaration)
	}
	if len(tb.Partitioning.Key) != 0 {
		t.Errorf("Key = %v, want EMPTY — nothing in a comment is a partition key", tb.Partitioning.Key)
	}
}

// MySQL's KEY strategy admits an ALGORITHM option between the strategy word
// and the key list ("PARTITION BY KEY ALGORITHM=2 (id)"). That is a form this
// reducer does not decompose: reporting "key algorithm=2" as the strategy
// would invent a vocabulary word no dialect has. The strategy abstains; the
// declaration still reports the clause, so the abstention is visible.
func TestSQLDDL_MySQL_UndecomposedStrategyForm_AbstainsButStillDeclares(t *testing.T) {
	s := parsePart(t, sqlddl.New(sqlddl.WithDialect(sqlddl.MySQL())),
		"CREATE TABLE `k` (`id` INT NOT NULL, PRIMARY KEY (`id`)) PARTITION BY KEY ALGORITHM=2 (`id`) PARTITIONS 4;\n")
	tb := partTable(t, s, "k")
	if got := tb.Partitioning.Strategy; got != "" {
		t.Errorf("Strategy = %q, want EMPTY — 'KEY ALGORITHM=2' is not a strategy word this reducer decomposes, and reporting it as one invents vocabulary", got)
	}
	if tb.Partitioning.Declaration == "" {
		t.Error("Declaration is empty — an undecomposed form must still be reported, never dropped silently")
	}
}

// The T-SQL "ON <scheme>(<col>)" read is gated on the PartitionSchemeOnClause
// dialect DATUM. Parsing the identical text under PostgreSQL must read no
// partitioning: PostgreSQL's CREATE TABLE tail has no partition-scheme
// grammar, and "ON" there belongs to entirely different clauses (ON COMMIT).
func TestSQLDDL_PG_OnClause_IsNotReadAsAPartitionScheme(t *testing.T) {
	s := parsePart(t, sqlddl.New(), `
CREATE TABLE orders (order_id int NOT NULL, order_date date NOT NULL) ON somescheme(order_date);
`)
	tb := partTable(t, s, "orders")
	if tb.Partitioning.Declaration != "" || tb.Partitioning.Scheme != "" {
		t.Errorf("Partitioning = %+v, want the zero value — PostgreSQL has no partition-scheme ON clause, and the dialect datum gates the read", tb.Partitioning)
	}
}
