package dbrules

import (
	"strconv"
	"strings"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// All four rules in this file are PURE SURFACE — never affirmations (ADR 0017).
// They read meaning from names, which is inherently noisy, so they expose facts
// and let the agent judge. Anti-noise levers: a type filter, component-based name
// matching (not substring), and fact-not-judgment. A counter-signal in a name
// (e.g. "hash") is EXPOSED as a fact, never used to suppress (Adjustment 1).

// db051 — a foreign key typed as text instead of a numeric/uuid key. This is a
// structural type-mismatch check, not a name guess: it fires only when a String/
// Text FK references a NUMERIC key, or when the key is an unbounded @db.Text — so
// a String FK to a String uuid PK does not fire, and neither does an unresolvable
// reference (conservative). Lowest-FP of the four.
type db051 struct{}

func (db051) ID() string { return "DB-051" }

func (db051) Check(s *db.Schema) ([]findings.Finding, []findings.SurfaceItem) {
	cols := columnIndex(s)
	var out []findings.SurfaceItem
	for _, t := range s.Tables {
		local := cols[t.Name]
		for _, fk := range t.ForeignKeys {
			mismatch, textKey, resolved := false, false, false
			for i, fkc := range fk.Columns {
				c, ok := local[fkc]
				if !ok {
					continue
				}
				if c.Type == db.TypeText || strings.Contains(c.RawType, "@db.Text") {
					textKey = true
				}
				if refCols := cols[fk.RefTable]; refCols != nil && i < len(fk.RefColumns) {
					if rc, ok := refCols[fk.RefColumns[i]]; ok {
						resolved = true
						if (c.Type == db.TypeString || c.Type == db.TypeText) && rc.Type == db.TypeInt {
							mismatch = true
						}
					}
				}
			}
			if !mismatch && !textKey {
				continue
			}
			out = append(out, findings.SurfaceItem{
				Category: string(surface.CategoryDBFKTextType),
				File:     fk.Pos.File,
				Line:     fk.Pos.Line,
				StructuralSignals: []string{
					"fk_columns: " + strings.Join(fk.Columns, ", "),
					"referenced: " + fk.RefTable + "." + strings.Join(fk.RefColumns, ", "),
				},
				StructuralFacts: map[string]bool{
					"type_mismatch":            mismatch,
					"text_key":                 textKey,
					"referenced_type_resolved": resolved,
				},
				ReasonToReview: "This foreign key is typed as text while the referenced key is numeric (or is an " +
					"unbounded TEXT). Is this an intentional string id (uuid/cuid), or a mistyped / inefficient key?",
			})
		}
	}
	return nil, out
}

// db052 — a table with NEITHER createdAt NOR updatedAt. Fires only when BOTH are
// missing (the clearest smell); "only one missing" is a DEFERRED candidate
// (DB-052b) to decide with dogfood evidence, not a permanent exclusion. It exposes
// looks_like_join_table so the agent can dismiss link tables — it does not suppress.
type db052 struct{}

func (db052) ID() string { return "DB-052" }

func (db052) Check(s *db.Schema) ([]findings.Finding, []findings.SurfaceItem) {
	var out []findings.SurfaceItem
	for _, t := range s.Tables {
		if !t.StructureProven() {
			// ABSTAIN (D4, design SS4): a dropped ADD COLUMN might have
			// declared created_at/updated_at.
			continue
		}
		hasCreated, hasUpdated := false, false
		names := make([]string, 0, len(t.Columns))
		for _, c := range t.Columns {
			names = append(names, c.Name)
			switch normalizeIdent(c.Name) {
			case "createdat":
				hasCreated = true
			case "updatedat":
				hasUpdated = true
			}
		}
		if hasCreated || hasUpdated {
			continue
		}
		out = append(out, findings.SurfaceItem{
			Category: string(surface.CategoryDBNoTimestamps),
			File:     t.Pos.File,
			Line:     t.Pos.Line,
			StructuralSignals: []string{
				"table: " + t.Name,
				"columns: " + strings.Join(names, ", "),
			},
			StructuralFacts: map[string]bool{
				"has_created_at":        false,
				"has_updated_at":        false,
				"looks_like_join_table": looksLikeJoinTable(t),
			},
			ReasonToReview: "This table has no created/updated audit timestamps. Should it track them, or is it a " +
				"link/config table where their absence is intentional?",
		})
	}
	return nil, out
}

// db053 — a column whose name suggests sensitive data, stored in a plain
// secret-holding type (String/Text/Bytes). It ALWAYS emits when the name+type
// match (Adjustment 1): a name carrying an encryption hint (hash/encrypted/…) is
// reported as the fact encryption_hint_in_name, never suppressed — a name is not a
// guarantee, and hiding a possible plaintext secret would be a silent false
// negative (ADR 0005).
type db053 struct{}

func (db053) ID() string { return "DB-053" }

func (db053) Check(s *db.Schema) ([]findings.Finding, []findings.SurfaceItem) {
	var out []findings.SurfaceItem
	for _, t := range s.Tables {
		for _, c := range t.Columns {
			if !storableSecretType(c.Type) {
				continue
			}
			tok, ok := matchSensitiveToken(c.Name)
			if !ok {
				continue
			}
			out = append(out, findings.SurfaceItem{
				Category: string(surface.CategoryDBSensitiveUnencrypted),
				File:     c.Pos.File,
				Line:     c.Pos.Line,
				StructuralSignals: []string{
					"column: " + c.Name,
					"type: " + c.RawType,
					"matched_token: " + tok,
				},
				StructuralFacts: map[string]bool{
					"encryption_hint_in_name": hasEncryptionHint(c.Name),
				},
				ReasonToReview: "A column whose name suggests sensitive data is stored in a plain column type. Is the " +
					"value encrypted/hashed at rest, or exposed in plaintext? (encryption_hint_in_name is a hint from " +
					"the name, not a guarantee.)",
			})
		}
	}
	return nil, out
}

// db003 — a repeating group: two or more columns of the SAME type sharing a base
// name with a numeric suffix (phone1/phone2/phone3), a 1NF smell. It fires on the
// structural pattern; the agent judges whether it is a violation or an intentional
// fixed set (e.g. address line 1/2).
type db003 struct{}

func (db003) ID() string { return "DB-003" }

func (db003) Check(s *db.Schema) ([]findings.Finding, []findings.SurfaceItem) {
	var out []findings.SurfaceItem
	for _, t := range s.Tables {
		groups := map[string][]db.Column{}
		var order []string
		for _, c := range t.Columns {
			base, ok := numericSuffixBase(c.Name)
			if !ok {
				continue
			}
			if _, seen := groups[base]; !seen {
				order = append(order, base)
			}
			groups[base] = append(groups[base], c)
		}
		for _, base := range order {
			members := groups[base]
			if len(members) < 2 || !uniformType(members) {
				continue
			}
			names := make([]string, len(members))
			anchor := members[0].Pos
			for i, m := range members {
				names[i] = m.Name
				if m.Pos.Line > 0 && (anchor.Line == 0 || m.Pos.Line < anchor.Line) {
					anchor = m.Pos
				}
			}
			out = append(out, findings.SurfaceItem{
				Category: string(surface.CategoryDBRepeatingGroups),
				File:     anchor.File,
				Line:     anchor.Line,
				StructuralSignals: []string{
					"base: " + base,
					"columns: " + strings.Join(names, ", "),
					"count: " + strconv.Itoa(len(members)),
					"table: " + t.Name,
				},
				StructuralFacts: map[string]bool{"uniform_type": true},
				ReasonToReview: "Several columns share a base name with numeric suffixes (a repeating group). Is this a " +
					"1NF violation that should be a child table, or an intentional fixed set (e.g. address line 1/2)?",
			})
		}
	}
	return nil, out
}

// --- shared helpers ---

// columnIndex maps table name -> column name -> Column, for cross-table lookups.
func columnIndex(s *db.Schema) map[string]map[string]db.Column {
	out := make(map[string]map[string]db.Column, len(s.Tables))
	for _, t := range s.Tables {
		m := make(map[string]db.Column, len(t.Columns))
		for _, c := range t.Columns {
			m[c.Name] = c
		}
		out[t.Name] = m
	}
	return out
}

func storableSecretType(t db.Type) bool {
	return t == db.TypeString || t == db.TypeText || t == db.TypeBytes
}

// normalizeIdent lowercases and drops separators, so createdAt / created_at both
// normalize to "createdat".
func normalizeIdent(s string) string {
	return strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(s))
}

// looksLikeJoinTable reports a composite-PK table whose every column is part of
// the primary key or a foreign key (no data columns) — a link table where missing
// timestamps are usually intentional.
func looksLikeJoinTable(t db.Table) bool {
	if len(t.PrimaryKey) < 2 {
		return false
	}
	key := map[string]bool{}
	for _, c := range t.PrimaryKey {
		key[c] = true
	}
	for _, fk := range t.ForeignKeys {
		for _, c := range fk.Columns {
			key[c] = true
		}
	}
	for _, c := range t.Columns {
		if !key[c.Name] {
			return false
		}
	}
	return true
}

// numericSuffixBase returns the base of a name ending in digits (phone1 -> phone),
// and false when there is no trailing digit run or no base before it.
func numericSuffixBase(name string) (string, bool) {
	i := len(name)
	for i > 0 && name[i-1] >= '0' && name[i-1] <= '9' {
		i--
	}
	if i == len(name) || i == 0 {
		return "", false
	}
	return name[:i], true
}

func uniformType(cols []db.Column) bool {
	for i := 1; i < len(cols); i++ {
		if cols[i].Type != cols[0].Type {
			return false
		}
	}
	return true
}
