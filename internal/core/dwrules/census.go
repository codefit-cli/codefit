package dwrules

import (
	"github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/paradigm"
)

// Shared machinery for the SCHEMA-LEVEL CENSUS judgments — DW-005, DW-011 and
// DW-020. All three emit AT MOST ONE item for the whole schema, so all three
// answer the completeness question the same way (ADR 0034 §2.5): abstain as a
// WHOLE when a census member is unproven, never per table, because a shrunken
// census that still emits looks authoritative over an undercounted schema.
//
// # WHY THIS FILE EXISTS: ONE PREDICATE PER RULE, CONSULTED TWICE
//
// ADR 0038 established the load-bearing half of the idiom for DW-020: the gate
// and the census loop must read the SAME membership predicate, so a table can
// never be censused without being gated, nor gated without being censused. It
// deliberately did NOT retrofit DW-005 and DW-011, which each spelled their
// membership condition inline in both of their loops.
//
// That divergence then produced the exact defect it forbids. ADR 0038 §4
// measured it and left it open: adding one fact-role PARTITION OF child to a
// star makes DW-005 vanish, because DW-005's gate covered every fact- and
// dimension-role table while a partition child is unproven BY CONSTRUCTION.
// A dimension-role child does the same to DW-011 — which ADR 0038 believed
// safe ("DW-011's gate reads dimension-role tables only, so a fact-role child
// never reaches it": true, and incomplete, since a DIMENSION can be
// partitioned too and its child then earns a dimension role of its own).
//
// So the three rules now share censusAbstains, and each passes the ONE
// membership predicate its own census loop uses. See ADR 0039.
//
// # WHAT A PARTITION CHILD IS, AND WHY IT IS NOT A CENSUS MEMBER
//
// PostgreSQL's `CREATE TABLE c PARTITION OF p FOR VALUES ...` declares the
// child's partition BOUNDS and nothing else: its columns, primary key and
// constraints all live on the parent and appear nowhere in it. The reducer
// therefore marks it unproven under db.ReasonPartitionChildInheritsStructure —
// on a schema codefit read PERFECTLY, with zero dropped statements. That is
// the fourth Reason* value and the only one that is not a measurement failure.
//
// A census judgment must not treat it as one. Nothing was dropped, so nothing
// is missing; and a child is a RESTATEMENT of its parent, never an independent
// table to count. Both halves follow from the same fact and are enforced by
// the same predicate.
type censusMember func(db.Table) bool

// isPartitionChild reports whether the SOURCE declares t as a partition of
// another table. db.Partitioning.Of is the model's own back-reference, and the
// predicate keys on it rather than on db.Table.Note (ADR 0038 §3): Note is a
// human-facing inventory string, deduplicated by reason and concatenated, so
// branching on its CONTENT would turn ADR 0034 §2.8's measurement channel into
// a control channel and would break the moment a child accumulated a second
// reason.
//
// DECLARED LIMIT, inherited from the model: an empty Of is NOT proof that a
// table is not a partition. A child attached by `ALTER TABLE ... ATTACH
// PARTITION`, or dumped as a standalone CREATE TABLE with no partition grammar
// of its own — which is what pg_dump actually emits — is indistinguishable
// here from an ordinary table. Such a child is fully read and PROVEN, so it
// never interacts with the gate; but if it earns a warehouse role it IS
// censused. See db.Partitioning's type doc.
func isPartitionChild(t db.Table) bool { return t.Partitioning.Of != "" }

// censusAbstains reports whether a schema-level census judgment must abstain
// as a WHOLE: true when ANY census MEMBER is structurally unproven (ADR 0034
// §2.5).
//
// member MUST be the same predicate the caller's census loop consults. That is
// the whole point of routing both through this function: scoping the gate to
// members is what keeps a table the rule never concludes over from silencing
// the rule. ADR 0034's invariant is unweakened — "I did not see X on table T,
// therefore T lacks X" is still sound only over a proven T — because a
// non-member is never a T.
func censusAbstains(s *db.Schema, member censusMember) bool {
	for _, t := range s.Tables {
		if member(t) && !t.StructureProven() {
			return true
		}
	}
	return false
}

// hasWarehouseRole reports whether cls gave t a fact or dimension role. Roles
// are always READ from the classification and never re-derived (locked
// decision A5 / ADR 0033); a closed schema gate (ADR 0037) therefore leaves
// every census empty and every census rule silent, with no per-rule handling.
func hasWarehouseRole(t db.Table, cls *paradigm.Classification) bool {
	switch cls.Roles[t.Name] {
	case paradigm.RoleFact, paradigm.RoleDimension:
		return true
	default:
		return false
	}
}
