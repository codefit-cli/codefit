package dwrules

import (
	"strings"

	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// dw021 — a FACT-role table with no dialect-recognized columnar/analytic
// index. A fact table is the table BRIN/columnar indexing exists for: it is
// typically append-heavy, naturally ordered by insertion or time, and
// scanned in large aggregate ranges rather than looked up by a single key —
// the shape a plain B-tree serves worst and a columnar/block-range index
// serves best. Whether the ABSENCE matters depends on the table's real size
// and query pattern, which codefit cannot see from static DDL alone — so
// this is SURFACE, never an affirmation (ADR 0017), the same discipline
// dw001/dw010 already apply.
//
// VOCABULARY is deliberately small and PostgreSQL-only today: brin and gin,
// read from db.Index.Method (an unexported var below). Extending to another
// dialect's own columnar syntax is a vocabulary change only — it never
// touches this rule's control flow (task 3.3).
//
// SECOND-ORDER CONSEQUENCE, stated in-code rather than left implicit (S3
// design note, obs #1293): there is NO separate index-level incompleteness
// channel here. db.Index.Method is either (a) genuinely, provably empty — no
// USING clause was present, the honest default/unspecified case, the same
// convention as Table.DBName — or (b) inherits the ENCLOSING TABLE's
// completeness through the ordinary t.StructureProven() gate below. A
// dropped CREATE INDEX statement already marks the whole table unproven at
// the statement level (builder.applyCreateIndex -> MarkUnproven); this rule
// does not invent a second signal for the same fact (ADR 0034 §2.9: exactly
// two carriers, no third channel invented).
//
// DECLARED LIMIT 1 (T-SQL): SQL Server's analytic indexing is a SEPARATE
// statement, CREATE [CLUSTERED] COLUMNSTORE INDEX, which the SQL-DDL
// reducer's dispatcher does not recognize at all (apply()'s switch has no
// branch for it). This rule is Method-signal-based and cannot see a T-SQL
// columnstore index today — a T-SQL fact table with a real columnstore index
// still fires this rule, a false surface stated here and in dbcoverage.go
// rather than silently mis-cleared.
//
// DECLARED LIMIT 2 (MySQL — a DIALECT-WIDE abstain, the architect's explicit
// decision layered on top of the ADR 0034 per-table gate): MySQL's CREATE
// INDEX grammar allows "USING BTREE|HASH" in a SECOND position — an
// index_option AFTER the column list — that the SQL-DDL reducer's regex does
// not capture (only the PRE-column-list position is read). Method would be
// SYSTEMATICALLY empty on MySQL even when a method was genuinely declared.
// Answering "no columnar index" from that structural blindness would be
// concluding from parser silence — the exact class ADR 0034 exists to
// eliminate — even though MySQL genuinely has no BRIN/GIN vocabulary to miss
// either way. That semantic argument does NOT license answering from
// non-observation, so this rule ABSTAINS THE WHOLE CHECK whenever
// Schema.Dialect=="mysql", checked ONCE up front rather than per table: a
// schema is parsed under exactly one dialect at a time (ADR 0018, "the
// parser binds a single dialect per project at construction"), so the
// blindness is uniform across every table in it, and a per-table repeat of
// the same schema-wide fact would only obscure that it IS schema-wide.
type dw021 struct{}

func (dw021) ID() string { return "DW-021" }

// columnarMethods are the recognized analytic/columnar index access methods,
// lowercased to match db.Index.Method's own normalization (the SQL-DDL
// reducer lowercases every captured method before storing it).
var columnarMethods = map[string]bool{
	"brin": true,
	"gin":  true,
}

func (dw021) Check(s *db.Schema, cls *paradigm.Classification) ([]findings.Finding, []findings.SurfaceItem) {
	if s.Dialect == "mysql" {
		// ABSTAIN (declared limit 2, above).
		return nil, nil
	}
	var out []findings.SurfaceItem
	for _, t := range s.Tables {
		if cls.Roles[t.Name] != paradigm.RoleFact {
			continue
		}
		if !t.StructureProven() {
			// ABSTAIN (ADR 0034): a dropped statement affecting this table
			// might have declared the very columnar index this rule asks
			// about.
			continue
		}
		if anyColumnarIndex(t) {
			continue
		}
		out = append(out, findings.SurfaceItem{
			Category: string(surface.CategoryDWNoColumnarIndex),
			File:     t.Pos.File,
			Line:     t.Pos.Line,
			StructuralSignals: []string{
				"table: " + t.Name,
				"existing_index_methods: " + describeIndexMethods(t.Indexes),
			},
			StructuralFacts: map[string]bool{
				"columnar_index_detected": false,
				"has_any_index":           len(t.Indexes) > 0,
			},
			ReasonToReview: "Fact table " + t.Name + " has no index using a recognized columnar/analytic " +
				"method (brin, gin). Fact tables are typically append-heavy and scanned in large ranges " +
				"rather than looked up by key — the shape a columnar or block-range index serves well and a " +
				"plain B-tree serves poorly. Given this table's real size and query pattern, would a columnar " +
				"index help? (SQL Server's CREATE COLUMNSTORE INDEX is a separate statement this rule cannot " +
				"see — a real columnstore index there would still surface here.)",
		})
	}
	return nil, out
}

// anyColumnarIndex reports whether t carries at least one index whose Method
// is in the recognized columnar vocabulary.
func anyColumnarIndex(t db.Table) bool {
	for _, idx := range t.Indexes {
		if columnarMethods[idx.Method] {
			return true
		}
	}
	return false
}

// describeIndexMethods renders a table's index methods for a signal, stating
// the empty (no indexes at all) and unspecified (index exists, no USING
// clause) cases explicitly rather than emitting a blank value.
func describeIndexMethods(indexes []db.Index) string {
	if len(indexes) == 0 {
		return "(none)"
	}
	parts := make([]string, 0, len(indexes))
	for _, idx := range indexes {
		method := idx.Method
		if method == "" {
			method = "(unspecified)"
		}
		parts = append(parts, method)
	}
	return strings.Join(parts, ", ")
}
