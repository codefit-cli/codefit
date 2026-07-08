package sqlddl

import (
	"regexp"
	"strings"

	"github.com/codefit-cli/codefit/internal/core/db"
)

// builder accumulates the mutable schema state as statements are applied in
// order. It is dialect-free CODE (design §1): the dialect only supplies
// type/modifier VOCABULARY (dialect.TypeMap / dialect.Modifiers) — quoting was
// already canonicalized by split() before apply() ever sees a statement.
type builder struct {
	order     []string
	tables    map[string]*db.Table
	views     []db.View
	procs     []db.Procedure
	trigs     []db.Trigger
	seenIndex map[string]bool
	dialect   *Dialect
}

func newBuilder(dialect *Dialect) *builder {
	return &builder{tables: map[string]*db.Table{}, seenIndex: map[string]bool{}, dialect: dialect}
}

func (b *builder) schema() *db.Schema {
	s := &db.Schema{Views: b.views, Procedures: b.procs, Triggers: b.trigs}
	for _, name := range b.order {
		if t := b.tables[name]; t != nil {
			s.Tables = append(s.Tables, *t)
		}
	}
	return s
}

var (
	reCreateTable = regexp.MustCompile(`(?is)^create\s+table\s+(if\s+not\s+exists\s+)?("?[\w".]+"?)\s*\(`)
	reAlterTable  = regexp.MustCompile(`(?is)^alter\s+table\s+(?:if\s+exists\s+)?("?[\w".]+"?)\s+(.*)$`)
	reCreateIndex = regexp.MustCompile(`(?is)^create\s+(unique\s+)?index\s+(?:concurrently\s+)?(if\s+not\s+exists\s+)?("?[\w"]+"?)\s+on\s+("?[\w".]+"?)\s*(?:using\s+\w+\s*)?\(([^)]*)\)`)
	reView        = regexp.MustCompile(`(?is)^create\s+(?:or\s+replace\s+)?(?:materialized\s+)?view\s+(?:if\s+not\s+exists\s+)?("?[\w".]+"?)`)
	reRoutine     = regexp.MustCompile(`(?is)^create\s+(?:or\s+replace\s+)?(?:function|procedure)\s+("?[\w".]+"?)`)
	reTrigger     = regexp.MustCompile(`(?is)^create\s+(?:or\s+replace\s+)?(?:constraint\s+)?trigger\s+("?[\w"]+"?)\b.*?\son\s+("?[\w".]+"?)`)
	reDropTable   = regexp.MustCompile(`(?is)^drop\s+table\s+(?:if\s+exists\s+)?("?[\w".]+"?)`)
	reReferences  = regexp.MustCompile(`(?is)references\s+("?[\w".]+"?)\s*(?:\(([^)]*)\))?`)
)

// apply classifies one statement and mutates the schema. Anything outside the
// declared subset (INSERT/UPDATE/DO/GRANT/COMMENT/CREATE TYPE/…) is skipped.
func (b *builder) apply(file string, st stmt) {
	pos := db.Pos{File: file, Line: st.line}
	head := strings.ToLower(strings.TrimSpace(st.text))
	switch {
	case reCreateTable.MatchString(st.text):
		b.applyCreateTable(file, st)
	case strings.HasPrefix(head, "alter table"):
		b.applyAlterTable(file, st)
	case reCreateIndex.MatchString(st.text):
		b.applyCreateIndex(file, st)
	case reView.MatchString(st.text):
		b.views = append(b.views, db.View{Name: normalizeName(reView.FindStringSubmatch(st.text)[1]), Pos: pos})
	case reRoutine.MatchString(st.text):
		b.procs = append(b.procs, db.Procedure{Name: routineName(reRoutine.FindStringSubmatch(st.text)[1]), Pos: pos})
	case reTrigger.MatchString(st.text):
		m := reTrigger.FindStringSubmatch(st.text)
		b.trigs = append(b.trigs, db.Trigger{Name: normalizeName(m[1]), Pos: pos, Table: normalizeName(m[2])})
	case reDropTable.MatchString(st.text):
		b.dropTable(normalizeName(reDropTable.FindStringSubmatch(st.text)[1]))
	default:
		// out of the declared subset — skipped on purpose.
	}
}

func (b *builder) getTable(name string) *db.Table {
	t := b.tables[name]
	if t == nil {
		t = &db.Table{Name: name}
		b.tables[name] = t
		b.order = append(b.order, name)
	}
	return t
}

func (b *builder) dropTable(name string) {
	if _, ok := b.tables[name]; !ok {
		return
	}
	delete(b.tables, name)
	kept := b.order[:0]
	for _, n := range b.order {
		if n != name {
			kept = append(kept, n)
		}
	}
	b.order = kept
}

func (b *builder) applyCreateTable(file string, st stmt) {
	loc := reCreateTable.FindStringSubmatchIndex(st.text)
	name := normalizeName(st.text[loc[4]:loc[5]])
	ifNotExists := loc[2] != -1
	if _, exists := b.tables[name]; exists {
		// AJUSTE 1: an existing table + (implicit) IF NOT EXISTS → skip the WHOLE
		// statement. The first CREATE wins; no Frankenstein merge.
		_ = ifNotExists
		return
	}
	openIdx := loc[1] - 1 // the '(' the regex ended on
	inner, innerStart, ok := balancedParen(st.text, openIdx)
	if !ok {
		b.getTable(name) // malformed body: still register the table, no columns
		return
	}
	t := b.getTable(name)
	for _, p := range splitTopLevelParts(inner) {
		line := st.line + strings.Count(st.text[:innerStart+p.off], "\n")
		b.applyTableItem(t, p.text, db.Pos{File: file, Line: line})
	}
}

// applyTableItem parses one comma-separated item of a CREATE TABLE body: a table
// constraint or a column definition.
func (b *builder) applyTableItem(t *db.Table, item string, pos db.Pos) {
	item = strings.TrimSpace(item)
	up := strings.ToUpper(item)
	switch {
	case strings.HasPrefix(up, "CONSTRAINT "):
		// strip "CONSTRAINT <name>" then treat the rest as a table constraint
		rest := strings.TrimSpace(item[len("CONSTRAINT "):])
		if sp := strings.IndexAny(rest, " \t\n("); sp >= 0 {
			rest = strings.TrimSpace(rest[sp:])
		}
		b.applyTableConstraint(t, rest, pos)
	case strings.HasPrefix(up, "PRIMARY KEY"), strings.HasPrefix(up, "UNIQUE"),
		strings.HasPrefix(up, "FOREIGN KEY"), strings.HasPrefix(up, "CHECK"),
		strings.HasPrefix(up, "EXCLUDE"), strings.HasPrefix(up, "PARTITION"):
		b.applyTableConstraint(t, item, pos)
	default:
		b.applyColumn(t, item, pos)
	}
}

func (b *builder) applyTableConstraint(t *db.Table, c string, pos db.Pos) {
	up := strings.ToUpper(strings.TrimSpace(c))
	switch {
	case strings.HasPrefix(up, "PRIMARY KEY"):
		t.PrimaryKey = parenCols(c)
	case strings.HasPrefix(up, "UNIQUE"):
		t.Indexes = append(t.Indexes, db.Index{Pos: pos, Columns: parenCols(c), Unique: true})
	case strings.HasPrefix(up, "FOREIGN KEY"):
		if fk, ok := parseForeignKey(c, pos); ok {
			t.ForeignKeys = append(t.ForeignKeys, fk)
		}
	default:
		// CHECK / EXCLUDE / PARTITION — declared limits, skipped.
	}
}

func (b *builder) applyColumn(t *db.Table, def string, pos db.Pos) {
	def = strings.TrimSpace(def)
	name, rest := firstToken(def)
	if name == "" {
		return
	}
	rawType, mods := splitTypeAndMods(rest, b.dialect.Modifiers)
	col := db.Column{
		Name:    normalizeName(name),
		DBName:  "",
		Pos:     pos,
		RawType: strings.TrimSpace(rawType),
		Type:    b.dialect.mapType(typeBase(rawType)),
		List:    strings.Contains(rawType, "[]"),
	}
	upMods := strings.ToUpper(mods)
	col.Nullable = !strings.Contains(upMods, "NOT NULL") && !strings.Contains(upMods, "PRIMARY KEY")
	t.Columns = append(t.Columns, col)

	if strings.Contains(upMods, "PRIMARY KEY") {
		t.PrimaryKey = append(t.PrimaryKey, col.Name)
	}
	if containsWord(upMods, "UNIQUE") {
		t.Indexes = append(t.Indexes, db.Index{Pos: pos, Columns: []string{col.Name}, Unique: true})
	}
	if m := reReferences.FindStringSubmatch(mods); m != nil {
		t.ForeignKeys = append(t.ForeignKeys, db.ForeignKey{
			Pos: pos, Columns: []string{col.Name}, RefTable: normalizeName(m[1]), RefColumns: splitIdents(m[2]),
		})
	}
}

func (b *builder) applyAlterTable(file string, st stmt) {
	m := reAlterTable.FindStringSubmatch(st.text)
	if m == nil {
		return
	}
	t := b.getTable(normalizeName(m[1]))
	// offset of the action group within the statement, for per-action line numbers
	actOff := strings.Index(st.text, m[2])
	for _, p := range splitTopLevelParts(m[2]) {
		line := st.line + strings.Count(st.text[:actOff+p.off], "\n")
		b.applyAlterAction(t, strings.TrimSpace(p.text), db.Pos{File: file, Line: line})
	}
}

func (b *builder) applyAlterAction(t *db.Table, act string, pos db.Pos) {
	up := strings.ToUpper(act)
	switch {
	case strings.HasPrefix(up, "ADD CONSTRAINT"):
		rest := strings.TrimSpace(act[len("ADD CONSTRAINT"):])
		if sp := strings.IndexAny(rest, " \t\n("); sp >= 0 {
			rest = strings.TrimSpace(rest[sp:])
		}
		b.applyTableConstraint(t, rest, pos)
	case strings.HasPrefix(up, "ADD PRIMARY KEY"), strings.HasPrefix(up, "ADD UNIQUE"), strings.HasPrefix(up, "ADD FOREIGN KEY"):
		b.applyTableConstraint(t, strings.TrimSpace(act[len("ADD"):]), pos)
	case strings.HasPrefix(up, "ADD COLUMN"), strings.HasPrefix(up, "ADD "):
		rest := strings.TrimSpace(act[len("ADD"):])
		rest = trimPrefixFold(rest, "COLUMN")
		rest = trimPrefixFold(rest, "IF NOT EXISTS")
		name, _ := firstToken(rest)
		if hasColumn(t, normalizeName(name)) {
			return // idempotent add
		}
		b.applyColumn(t, rest, pos)
	case strings.HasPrefix(up, "DROP COLUMN"), strings.HasPrefix(up, "DROP "):
		rest := strings.TrimSpace(act[len("DROP"):])
		rest = trimPrefixFold(rest, "COLUMN")
		rest = trimPrefixFold(rest, "IF EXISTS")
		name, _ := firstToken(rest)
		dropColumn(t, normalizeName(name))
	default:
		// ALTER COLUMN / RENAME / OWNER / ENABLE … — declared limits, skipped.
	}
}

func (b *builder) applyCreateIndex(file string, st stmt) {
	m := reCreateIndex.FindStringSubmatch(st.text)
	if m == nil {
		return
	}
	unique := m[1] != ""
	name := normalizeName(m[3])
	if m[2] != "" && b.seenIndex[name] { // IF NOT EXISTS + already created
		return
	}
	b.seenIndex[name] = true
	t := b.getTable(normalizeName(m[4]))
	t.Indexes = append(t.Indexes, db.Index{Pos: db.Pos{File: file, Line: st.line}, Columns: splitIdents(m[5]), Unique: unique})
}

// --- small parse helpers ---

func parseForeignKey(c string, pos db.Pos) (db.ForeignKey, bool) {
	cols := parenCols(c)
	m := reReferences.FindStringSubmatch(c)
	if m == nil || len(cols) == 0 {
		return db.ForeignKey{}, false
	}
	return db.ForeignKey{Pos: pos, Columns: cols, RefTable: normalizeName(m[1]), RefColumns: splitIdents(m[2])}, true
}

// parenCols returns the identifiers inside the first (...) of s.
func parenCols(s string) []string {
	i := strings.IndexByte(s, '(')
	if i < 0 {
		return nil
	}
	inner, _, ok := balancedParen(s, i)
	if !ok {
		return nil
	}
	return splitIdents(inner)
}

func splitIdents(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		// drop trailing ASC/DESC and quotes
		if sp := strings.IndexByte(p, ' '); sp >= 0 {
			p = p[:sp]
		}
		if p = normalizeName(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// firstToken splits s into its first whitespace-delimited token and the rest.
func firstToken(s string) (string, string) {
	s = strings.TrimSpace(s)
	i := strings.IndexAny(s, " \t\n")
	if i < 0 {
		return s, ""
	}
	return s[:i], strings.TrimSpace(s[i:])
}

// splitTypeAndMods splits a column's tail into its type expression and the
// trailing modifiers, stopping the type at the first modifiers keyword at
// paren-depth 0. modifiers is the dialect's parse-and-ignore vocabulary
// (dialect.Modifiers) — this function itself stays dialect-free CODE.
func splitTypeAndMods(rest string, modifiers map[string]bool) (typeExpr, mods string) {
	depth := 0
	i := 0
	for i < len(rest) {
		if rest[i] == '(' {
			depth++
		} else if rest[i] == ')' {
			depth--
		} else if depth == 0 && (rest[i] == ' ' || rest[i] == '\t' || rest[i] == '\n') {
			word, _ := firstToken(rest[i:])
			if modifiers[strings.ToUpper(word)] {
				return strings.TrimSpace(rest[:i]), strings.TrimSpace(rest[i:])
			}
		}
		i++
	}
	return strings.TrimSpace(rest), ""
}

// part is a comma-separated fragment with its byte offset in the source.
type part struct {
	text string
	off  int
}

// splitTopLevelParts splits by commas at paren-depth 0, respecting single-quoted
// strings, returning each fragment with its offset.
func splitTopLevelParts(s string) []part {
	var out []part
	depth, start := 0, 0
	inStr := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inStr:
			if c == '\'' {
				inStr = false
			}
		case c == '\'':
			inStr = true
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == ',' && depth == 0:
			out = append(out, part{s[start:i], start})
			start = i + 1
		}
	}
	out = append(out, part{s[start:], start})
	return out
}

// balancedParen returns the content between the '(' at openIdx and its matching
// ')', the index just after '(', and ok. Respects single-quoted strings.
func balancedParen(s string, openIdx int) (inner string, innerStart int, ok bool) {
	if openIdx >= len(s) || s[openIdx] != '(' {
		return "", 0, false
	}
	depth := 0
	inStr := false
	for i := openIdx; i < len(s); i++ {
		c := s[i]
		switch {
		case inStr:
			if c == '\'' {
				inStr = false
			}
		case c == '\'':
			inStr = true
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 {
				return s[openIdx+1 : i], openIdx + 1, true
			}
		}
	}
	return "", 0, false
}

// normalizeName strips surrounding quotes and a schema qualifier (public.t -> t).
func normalizeName(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '.'); i >= 0 {
		s = s[i+1:]
	}
	s = strings.Trim(s, `"`)
	return strings.TrimSpace(s)
}

// routineName strips a function/procedure argument list: f(int) -> f.
func routineName(s string) string {
	if i := strings.IndexByte(s, '('); i >= 0 {
		s = s[:i]
	}
	return normalizeName(s)
}

func containsWord(s, word string) bool {
	for _, f := range strings.Fields(s) {
		if f == word {
			return true
		}
	}
	return false
}

func trimPrefixFold(s, prefix string) string {
	if len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix) {
		return strings.TrimSpace(s[len(prefix):])
	}
	return s
}

func hasColumn(t *db.Table, name string) bool {
	for _, c := range t.Columns {
		if c.Name == name {
			return true
		}
	}
	return false
}

func dropColumn(t *db.Table, name string) {
	kept := t.Columns[:0]
	for _, c := range t.Columns {
		if c.Name != name {
			kept = append(kept, c)
		}
	}
	t.Columns = kept
}
