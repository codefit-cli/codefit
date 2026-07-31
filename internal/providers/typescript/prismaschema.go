package typescript

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/providers"
)

// compile-time check that the TS provider implements the schema-parsing
// capability (resolved by type-assertion, ADR 0014).
var _ providers.SchemaParser = (*Provider)(nil)

// ParseSchema parses Prisma schema source(s) into the neutral db.Schema model. It
// is a hand-written, line-oriented parser over the Prisma DSL (not tree-sitter —
// ADR 0014), so it yields a Pos line for every element for free. It is TWO-PASS:
// a field's type can be a scalar, a model name (a virtual relation field —
// excluded as a column) or an enum name (a real column). Pass one collects the
// model and enum names; pass two resolves each field against those sets. Without
// it, an enum column (role Role) would be silently dropped as a relation.
//
// Prisma view blocks are out of scope this slice and are skipped without error
// (Schema.Views stays empty; the SQL-DDL parser of slice 3 fills views).
func (*Provider) ParseSchema(sources []providers.SourceFile) (*db.Schema, error) {
	var blocks []prismaBlock
	for _, s := range sources {
		bs, err := splitBlocks(s.Path, s.Content)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, bs...)
	}

	// Pass 1: collect the names of every model and enum across all files.
	models, enums := map[string]bool{}, map[string]bool{}
	for _, b := range blocks {
		switch b.kind {
		case "model":
			models[b.name] = true
		case "enum":
			enums[b.name] = true
		}
	}

	// Pass 2: build a table per model block; everything else is skipped.
	schema := &db.Schema{}
	for _, b := range blocks {
		if b.kind == "model" {
			schema.Tables = append(schema.Tables, parseModel(b, models, enums))
		}
	}
	return schema, nil
}

// prismaBlock is a brace-delimited top-level block with its body lines and their
// original 1-based line numbers.
type prismaBlock struct {
	kind string
	name string
	file string
	line int          // header line
	body []prismaLine // non-empty body lines, comments stripped
}

type prismaLine struct {
	num  int
	text string
}

// blockHeader matches a recognized block opener: `<kind> <name> {`. `view` is NOT
// recognized (out of scope this slice) — a view block falls through to lenient
// stray-line skipping and produces nothing (ADR 0014).
var blockHeader = regexp.MustCompile(`^(model|enum|datasource|generator|type)\s+([A-Za-z_]\w*)\s*\{`)

// splitBlocks scans the source into top-level blocks. A recognized block is
// brace-matched and its body collected; an unrecognized top-level line is skipped
// leniently (unknown directive or a view block). An unclosed recognized block is a
// hard error — the malformed-schema case codefit must not parse silently.
func splitBlocks(file string, content []byte) ([]prismaBlock, error) {
	lines := strings.Split(string(content), "\n")
	var blocks []prismaBlock
	for i := 0; i < len(lines); {
		head := strings.TrimSpace(stripComment(lines[i]))
		if head == "" {
			i++
			continue
		}
		m := blockHeader.FindStringSubmatch(head)
		if m == nil {
			i++ // stray top-level line (unknown directive / skipped view block)
			continue
		}
		kind, name, headerLine := m[1], m[2], i+1
		depth := braceDelta(head)
		var body []prismaLine
		i++
		closed := depth == 0
		for i < len(lines) && depth > 0 {
			raw := stripComment(lines[i])
			depth += braceDelta(raw)
			if depth == 0 {
				closed = true
				i++
				break
			}
			if t := strings.TrimSpace(raw); t != "" {
				body = append(body, prismaLine{num: i + 1, text: t})
			}
			i++
		}
		if !closed {
			return nil, fmt.Errorf("parsing prisma schema %q: unclosed %s block %q opened at line %d", file, kind, name, headerLine)
		}
		blocks = append(blocks, prismaBlock{kind: kind, name: name, file: file, line: headerLine, body: body})
	}
	return blocks, nil
}

func braceDelta(s string) int { return strings.Count(s, "{") - strings.Count(s, "}") }

// stripComment removes a `//` (or `///`) line comment, ignoring `//` that occurs
// inside a double-quoted string (e.g. a "postgresql://..." url).
func stripComment(line string) string {
	inStr := false
	for i := 0; i+1 < len(line); i++ {
		switch {
		case line[i] == '"':
			inStr = !inStr
		case !inStr && line[i] == '/' && line[i+1] == '/':
			return line[:i]
		}
	}
	return line
}

// fieldLine matches `name Type` with the type's optional `[]` (list) and `?`
// (nullable) markers; the remainder (group 3) is the attribute modifiers.
var fieldLine = regexp.MustCompile(`^([A-Za-z_]\w*)\s+([A-Za-z_]\w*(?:\[\])?\??)(.*)$`)

// parseModel turns a model block into a db.Table, resolving each field against the
// model/enum name sets (the two-pass mechanism).
func parseModel(b prismaBlock, models, enums map[string]bool) db.Table {
	// Complete starts true (N1, design §1-D1b): db.Table.Complete's zero
	// value is false (fail-closed), so this construction site must set it
	// explicitly or every Prisma project mutes the whole DB dimension. A
	// table is demoted to Complete=false only when a body line is neither a
	// recognized field nor a @@-attribute (see MarkUnproven below).
	t := db.Table{Name: b.name, Pos: db.Pos{File: b.file, Line: b.line}, Complete: true}
	for _, ln := range b.body {
		pos := db.Pos{File: b.file, Line: ln.num}
		if strings.HasPrefix(ln.text, "@@") {
			parseBlockAttr(&t, ln.text, pos)
			continue
		}
		m := fieldLine.FindStringSubmatch(ln.text)
		if m == nil {
			// A body line that is neither a @@-attribute nor a field line:
			// the parser does not know what it declared, so it cannot rule
			// out that it declared a key, an index or a column. Recording it
			// honours the same contract the SQL-DDL reducer does (ADR 0034,
			// design SS7b) — the neutral model, not the provider, defines
			// what "unproven" means. Distinct from the deferred, test-locked
			// debt at splitBlocks' stray TOP-LEVEL line skip (:92 — no
			// *db.Table in scope there, a different granularity).
			t.MarkUnproven(db.ReasonUnreducedTableStatement, ln.text, pos)
			continue
		}
		name, typeTok, rest := m[1], m[2], m[3]
		base, nullable, list := parseTypeToken(typeTok)
		switch {
		case models[base]:
			// Relation field — NOT a column. A FK exists only when @relation names
			// local fields (the owning side); a back-relation (posts Post[]) names
			// none and must invent no FK (AJUSTE 2).
			if fk, ok := relationFK(rest, base, pos); ok {
				t.ForeignKeys = append(t.ForeignKeys, fk)
			}
		case enums[base]:
			t.Columns = append(t.Columns, db.Column{Name: name, DBName: columnDBName(rest), Pos: pos, Type: db.TypeEnum, RawType: base, Nullable: nullable, List: list})
			applyFieldAttrs(&t, name, rest, pos)
		default:
			typ, raw := scalarType(base, rest)
			t.Columns = append(t.Columns, db.Column{Name: name, DBName: columnDBName(rest), Pos: pos, Type: typ, RawType: raw, Nullable: nullable, List: list})
			applyFieldAttrs(&t, name, rest, pos)
		}
	}
	return t
}

// parseTypeToken splits a type token into its base name and the nullable/list
// markers. Prisma writes Type[] (list) or Type? (nullable); both orders tolerated.
func parseTypeToken(tok string) (base string, nullable, list bool) {
	base = tok
	for {
		switch {
		case strings.HasSuffix(base, "?"):
			nullable = true
			base = strings.TrimSuffix(base, "?")
		case strings.HasSuffix(base, "[]"):
			list = true
			base = strings.TrimSuffix(base, "[]")
		default:
			return base, nullable, list
		}
	}
}

var dbNativeRe = regexp.MustCompile(`@db\.(\w+)`)

// scalarType maps a Prisma scalar to the neutral type and builds the verbatim
// RawType (including a @db.* native modifier when present, e.g. "String @db.Text"
// → TypeText, for DB-051). An unrecognized base type is TypeUnknown (honest).
func scalarType(base, rest string) (db.Type, string) {
	raw := base
	native := ""
	if m := dbNativeRe.FindStringSubmatch(rest); m != nil {
		native = m[1]
		raw = base + " @db." + native
	}
	if native == "Text" {
		return db.TypeText, raw
	}
	switch base {
	case "String":
		return db.TypeString, raw
	case "Int", "BigInt":
		return db.TypeInt, raw
	case "Float", "Decimal":
		return db.TypeFloat, raw
	case "Boolean":
		return db.TypeBool, raw
	case "DateTime":
		return db.TypeDateTime, raw
	case "Json":
		return db.TypeJSON, raw
	case "Bytes":
		return db.TypeBytes, raw
	default:
		return db.TypeUnknown, raw
	}
}

var (
	attrID     = regexp.MustCompile(`@id\b`)
	attrUnique = regexp.MustCompile(`@unique\b`)
)

// applyFieldAttrs records field-level @id (single-column PK) and @unique (single
// unique index). Block-level @@id/@@unique are handled by parseBlockAttr.
func applyFieldAttrs(t *db.Table, field, rest string, pos db.Pos) {
	if attrID.MatchString(rest) {
		t.PrimaryKey = append(t.PrimaryKey, field)
	}
	if attrUnique.MatchString(rest) {
		t.Indexes = append(t.Indexes, db.Index{Pos: pos, Columns: []string{field}, Unique: true})
	}
}

// parseBlockAttr handles the block-level attributes that define keys/indexes:
// @@id (composite PK), @@unique (composite unique index), @@index (composite
// index). Others (@@map, ...) are ignored.
func parseBlockAttr(t *db.Table, text string, pos db.Pos) {
	switch {
	case strings.HasPrefix(text, "@@map"):
		t.DBName = stringArg(text)
	case strings.HasPrefix(text, "@@id"):
		t.PrimaryKey = bracketList(text)
	case strings.HasPrefix(text, "@@unique"):
		t.Indexes = append(t.Indexes, db.Index{Pos: pos, Columns: bracketList(text), Unique: true})
	case strings.HasPrefix(text, "@@index"):
		t.Indexes = append(t.Indexes, db.Index{Pos: pos, Columns: bracketList(text), Unique: false, Method: indexTypeArg(text)})
	}
}

var (
	stringArgRe = regexp.MustCompile(`"([^"]*)"`)
	colMapRe    = regexp.MustCompile(`(?:^|[^@])@map\(\s*"([^"]*)"\s*\)`)
)

// stringArg returns the first double-quoted argument of an attribute, e.g. the
// "accounts" of @@map("accounts"). Empty when absent.
func stringArg(text string) string {
	if m := stringArgRe.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	return ""
}

// columnDBName returns the remapped column name from a field's @map("name")
// attribute, or "" when there is none. The (?:^|[^@]) guard avoids matching the
// block-level @@map (which never reaches a field's modifiers anyway).
func columnDBName(rest string) string {
	if m := colMapRe.FindStringSubmatch(rest); m != nil {
		return m[1]
	}
	return ""
}

var (
	firstBracket = regexp.MustCompile(`\[([^\]]*)\]`)
	relFieldsRe  = regexp.MustCompile(`fields\s*:\s*\[([^\]]*)\]`)
	relRefsRe    = regexp.MustCompile(`references\s*:\s*\[([^\]]*)\]`)
	indexTypeRe  = regexp.MustCompile(`type\s*:\s*(\w+)`)
)

// indexTypeArg returns the access-method argument of a Prisma @@index(...,
// type: X) attribute, lowercased, or "" when no type: argument is present
// (index-method-capture, mirroring stringArg/relRefsRe's own convention of
// one small regex per attribute argument, next to those helpers).
//
// This is DELIBERATELY not validated against any assumed Prisma vocabulary:
// codefit has not verified Prisma's own documented set of accepted index
// types against every provider it supports, so whatever value the schema
// text declares is captured verbatim (only case-normalized) — parse what is
// there, never assume what SHOULD be there.
func indexTypeArg(text string) string {
	if m := indexTypeRe.FindStringSubmatch(text); m != nil {
		return strings.ToLower(m[1])
	}
	return ""
}

// bracketList returns the comma-separated identifiers inside the first [...] of s.
func bracketList(s string) []string {
	m := firstBracket.FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	return splitIdents(m[1])
}

// relationFK builds a ForeignKey from a relation field's @relation(fields:[...],
// references:[...]). It returns ok=false when there is no @relation or no local
// fields (a back-relation), so no phantom FK is created.
func relationFK(rest, refTable string, pos db.Pos) (db.ForeignKey, bool) {
	if !strings.Contains(rest, "@relation") {
		return db.ForeignKey{}, false
	}
	fm := relFieldsRe.FindStringSubmatch(rest)
	if fm == nil {
		return db.ForeignKey{}, false
	}
	fields := splitIdents(fm[1])
	if len(fields) == 0 {
		return db.ForeignKey{}, false
	}
	var refs []string
	if rm := relRefsRe.FindStringSubmatch(rest); rm != nil {
		refs = splitIdents(rm[1])
	}
	return db.ForeignKey{Pos: pos, Columns: fields, RefTable: refTable, RefColumns: refs}, true
}

func splitIdents(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
