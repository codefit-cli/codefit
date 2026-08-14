package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These two exported faces exist for ONE caller outside the audit: `codefit
// init`. The reason they live here, in the sensor, rather than in the scaffolder
// that calls them, is that this is where the ordering regex and the bucket rule
// already live. A copy of `^V(\d+)__` in internal/scaffold would be a second
// ordering-shape source — the same class of defect as a second parser-mapping
// site, one level down — and it would drift the first time flywayOrderedSQL
// learns another naming convention.

// writeFiles creates dir under root and writes each named file with its bytes.
// Fixtures are built as FILES ON DISK and read back through the real resolver:
// a hand-built resolution would lock a shape the production path cannot produce.
func writeFiles(t *testing.T, root, rel string, files map[string][]byte) string {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// utf16LEWithBOM encodes ASCII text as UTF-16LE with a byte-order mark — the
// shape PowerShell's redirection produces, and the exact shape that once reached
// the tokenizer as NUL-interleaved bytes and reduced to a schema with zero of
// everything and no complaint.
func utf16LEWithBOM(s string) []byte {
	out := []byte{0xFF, 0xFE}
	for _, r := range s {
		out = append(out, byte(r), 0x00)
	}
	return out
}

// TestResolveSchemaPath_DelegatesToTheScanTimeResolver is the reason
// ResolveSchemaPath is two statements rather than a reimplementation. Init must
// see the SAME bytes, in the SAME order, that the scan sees — so this asserts
// the two properties a reimplementation would be most likely to drop: integer
// version ordering (not lexical: V10 after V2) and the byte-order-mark decode.
func TestResolveSchemaPath_DelegatesToTheScanTimeResolver(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, "db/migrations", map[string][]byte{
		"V1__a.sql":  []byte("CREATE TABLE a (id INT);\n"),
		"V2__b.sql":  utf16LEWithBOM("CREATE TABLE b (id INT);\n"),
		"V10__c.sql": []byte("CREATE TABLE c (id INT);\n"),
	})

	files, err := ResolveSchemaPath(root, "db/migrations")
	if err != nil {
		t.Fatalf("ResolveSchemaPath: %v", err)
	}

	var got []string
	for _, f := range files {
		got = append(got, f.Path)
	}
	want := []string{"db/migrations/V1__a.sql", "db/migrations/V2__b.sql", "db/migrations/V10__c.sql"}
	if len(got) != len(want) {
		t.Fatalf("ResolveSchemaPath returned %d file(s) %v, want %d %v — init must read exactly what "+
			"the scan reads for this path", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("resolution order is %v, want %v — V10 after V2 is the INTEGER version order the "+
				"resolver proves; a lexical sort would put V10 first and init would prove a schema the "+
				"scan never reads", got, want)
		}
	}

	// The BOM decode is load-bearing, not incidental. Without it the UTF-16LE
	// file arrives NUL-interleaved, reconstructs nothing, and init and the scan
	// disagree about the same directory.
	bom := files[1]
	if strings.HasPrefix(string(bom.Content), "\xff\xfe") {
		t.Errorf("%s still carries its byte-order mark — ResolveSchemaPath did not go through the "+
			"resolver's sourcetext.Decode, so a PowerShell-written dump would prove differently at "+
			"init than at scan", bom.Path)
	}
	if !strings.Contains(string(bom.Content), "CREATE TABLE b") {
		t.Errorf("%s decoded to %q, which does not contain its own DDL — the UTF-16 content reached the "+
			"caller undecoded", bom.Path, string(bom.Content))
	}
}

// TestResolveSchemaPath_SingleFileAndMissingPath covers the two remaining shapes
// the scan-time resolver already defines: a path that is a FILE resolves to
// itself, and a path that does not exist is a hard error (a misconfiguration,
// never a silent "no DB").
func TestResolveSchemaPath_SingleFileAndMissingPath(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, "db", map[string][]byte{"schema.sql": []byte("CREATE TABLE a (id INT);\n")})

	files, err := ResolveSchemaPath(root, "db/schema.sql")
	if err != nil {
		t.Fatalf("ResolveSchemaPath on a file: %v", err)
	}
	if len(files) != 1 || files[0].Path != "db/schema.sql" {
		t.Fatalf("a file path resolved to %+v, want exactly itself", files)
	}

	if _, err := ResolveSchemaPath(root, "db/nowhere"); err == nil {
		t.Error("a configured path that does not exist must be an ERROR — returning an empty resolution " +
			"would let init silently treat a typo as 'this project has no schema'")
	}
}

// orderingCase is one directory shape and the verdict OrderingIsProven owes it.
type orderingCase struct {
	name   string
	files  []string
	proven bool
	sql    int
	why    string
}

// orderingCases are the shapes spec R1 turns on. They are stated once and used
// by BOTH the verdict test and the equivalence lock below, so the two cannot
// disagree about what they are talking about.
var orderingCases = []orderingCase{
	{
		name: "flyway integer versions", files: []string{"V1__a.sql", "V2__b.sql", "V10__c.sql"},
		proven: true, sql: 3,
		why: "every file carries an integer version, so the order is proven rather than assumed",
	},
	{
		name: "one stray unversioned file", files: []string{"V1__a.sql", "seed.sql"},
		proven: false, sql: 2,
		why: "seed.sql lands in the LEXICAL bucket; once one file is ordered by name the apply order " +
			"of the directory is no longer proven",
	},
	{
		name: "golang-migrate naming", files: []string{"1_init.up.sql", "1_init.down.sql", "10_x.up.sql"},
		proven: false, sql: 3,
		why: "golang-migrate names match no version regex here; every file is lexical, so 10_x sorts " +
			"before 1_init and the apply order is a guess",
	},
	{
		name: "dotted version", files: []string{"V1.1__a.sql"},
		proven: false, sql: 1,
		why: "the DECLARED LIMIT: V1.1 does not match ^V(\\d+)__ and falls to the lexical bucket",
	},
	{
		name: "no sql at all", files: []string{"README.md"},
		proven: false, sql: 0,
		why: "an empty level is not a proven ordering of nothing; it is nothing to order",
	},
	{
		name: "uppercase extension still counts", files: []string{"V1__a.SQL", "V2__b.sql"},
		proven: true, sql: 2,
		why: "the resolver matches .sql case-insensitively, so the proof must too or the two disagree " +
			"about which files exist",
	},
	{
		name: "nested sql does not count at this level", files: []string{"V1__a.sql", "sub/V2__b.sql"},
		proven: true, sql: 1,
		why: "discovery depth is not read depth: the resolver reads ONE level, so the proof reports on " +
			"one level and the nested file is invisible to both",
	},
}

func TestOrderingIsProven_VerdictPerShape(t *testing.T) {
	for _, tc := range orderingCases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "d")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			for _, name := range tc.files {
				full := filepath.Join(dir, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, []byte("-- x\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			proven, sqlFiles, err := OrderingIsProven(dir)
			if err != nil {
				t.Fatalf("OrderingIsProven: %v", err)
			}
			if proven != tc.proven {
				t.Errorf("OrderingIsProven(%v) = %v, want %v — %s", tc.files, proven, tc.proven, tc.why)
			}
			if sqlFiles != tc.sql {
				t.Errorf("OrderingIsProven(%v) counted %d .sql file(s) at this level, want %d",
					tc.files, sqlFiles, tc.sql)
			}
		})
	}
}

// C-EQ — the equivalence lock.
//
// OrderingIsProven re-applies flywayVersion instead of sharing flywayOrderedSQL's
// loop, because extracting a shared helper would edit sources.go and that file is
// kept byte-identical as this change's R10 evidence. A duplication accepted on
// purpose is only acceptable while it is LOCKED, so this asserts the equivalence
// itself rather than either side of it:
//
//	OrderingIsProven(dir) == true  ⟺  flywayOrderedSQL(dir) is the pure integer-version
//	                                  order, with the lexical bucket unused and non-empty input.
//
// The lexical bucket is what "not proven" MEANS here: the moment one file sorts
// by name, the apply order of the directory stopped being a fact about its
// contents and became a fact about the alphabet.
func TestOrderingIsProven_AgreesWithTheResolversOrdering(t *testing.T) {
	for _, tc := range orderingCases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "d")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			for _, name := range tc.files {
				full := filepath.Join(dir, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(full, []byte("-- x\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			proven, _, err := OrderingIsProven(dir)
			if err != nil {
				t.Fatalf("OrderingIsProven: %v", err)
			}

			// The right-hand side, computed from the REAL resolver's own output.
			ordered, err := flywayOrderedSQL(dir)
			if err != nil {
				t.Fatalf("flywayOrderedSQL: %v", err)
			}
			bucketUnused := len(ordered) > 0
			for _, p := range ordered {
				if !flywayVersion.MatchString(filepath.Base(p)) {
					bucketUnused = false
					break
				}
			}

			if proven != bucketUnused {
				t.Errorf("OrderingIsProven=%v but the resolver's own ordering says lexical-bucket-unused=%v "+
					"for %v (resolver order: %v).\nThese two must not drift: OrderingIsProven exists to say, "+
					"BEFORE anything is written to a config, whether flywayOrderedSQL will order this "+
					"directory by proof or by the alphabet. %s",
					proven, bucketUnused, tc.files, ordered, tc.why)
			}
		})
	}
}
