package db

import (
	"fmt"
	"sort"
	"strings"

	coredb "github.com/codefit-cli/codefit/internal/core/db"
	"github.com/codefit-cli/codefit/internal/core/sourcetext"
	"github.com/codefit-cli/codefit/internal/providers"
)

// THE FLOOR THIS FILE IS.
//
// ADR 0034 made the neutral model honest about a statement it could not
// reduce, and ADR 0043 gave the CREATE TABLE family a floor so that a
// declaration no branch models is DECLARED rather than dropped. Both of them
// reason about the inside of a file codefit successfully READ.
//
// One level up there was no floor at all. A schema source whose bytes the
// tokenizer never recognized as a statement — a UTF-16 dump from a PowerShell
// pg_dump, a truncated file, a format nobody has written a branch for — landed
// nowhere: no drop, no abstention, no note, because nothing was ever dropped.
// It produced Measured=true, score 100, an empty note and zero of everything,
// which is INDISTINGUISHABLE from a clean bill of health.
//
// The rule here is deliberately written against the OUTCOME rather than
// against a list of encodings, for the reason ADR 0043 §2.1 gives for its own
// shape-not-list regex: an enumeration closes the cases that are already known
// and leaves the class open, and the class is the defect. A configured source
// that contributed NOTHING to the neutral model is declared, whatever the
// reason turns out to be.

// unreadReason is the CLOSED vocabulary of why a configured schema source
// contributed nothing (ADR 0034 §2.8's measurement/diagnostics boundary: the
// note states the FACT and what it cost, never which regex missed). The three
// values answer three different questions and must not be collapsed:
// reasonNotText and reasonNothingRecognized are the DEFECT (codefit was blind
// over content that exists); reasonNoDeclarations is LEGITIMATE (there was
// nothing to read), and is recorded anyway only so that "0 tables" is never
// ambiguous.
type unreadReason string

const (
	// reasonNotText: the bytes are not text codefit can read. Stated as the
	// POSITIVE observation that produced it (a NUL byte survived byte-order-mark
	// decoding) rather than as a guess about which encoding it was —
	// sourcetext.Decode deliberately refuses to sniff a BOM-less file, and this
	// note is what keeps that refusal from becoming silence.
	reasonNotText unreadReason = "codefit could not read it as text: NUL bytes survived byte-order-mark decoding, " +
		"which is what a UTF-16 or UTF-32 file saved WITHOUT a byte-order mark looks like. " +
		"Re-saving it as UTF-8 (or with a byte-order mark) makes it readable"
	// reasonNothingRecognized: the file decoded to perfectly ordinary text and
	// not one statement in it reached the model — not a table, view, routine or
	// trigger, and not even a statement recorded as unreducible.
	reasonNothingRecognized unreadReason = "codefit read it as text and recognized nothing in it at all — " +
		"no table, view, routine or trigger, and not one statement it could even record as unreducible"
	// reasonNoDeclarations: benign. Outside whitespace and comments the file is
	// empty, so there was nothing to recognize.
	reasonNoDeclarations unreadReason = "it declares nothing outside whitespace and comments"
)

// defect reports whether this reason means codefit was BLIND over content that
// exists — the two reasons that make "audited, 0 findings" a lie.
func (r unreadReason) defect() bool { return r != reasonNoDeclarations }

// unreadSource is one configured schema file that reached no rule.
type unreadSource struct {
	Path   string
	Reason unreadReason
}

// unreadSources returns, IN CONFIG ORDER, every configured source that
// contributed nothing to the parsed schema.
//
// "Contributed" is measured against the neutral model's own positions —
// including its HONEST-FAILURE carriers (Schema.Unreduced, Schema.Withheld,
// Table.Unreduced). A file whose only trace is "codefit could not reduce this
// statement" is emphatically NOT unread: it was read, and the reader was told.
// Reading the model rather than asking the parser is what makes this floor
// provider-agnostic and fail-closed in the sense of ADR 0034 §2.3 — a new
// SchemaParser cannot forget to implement it, because there is nothing for it
// to implement.
func unreadSources(sources []providers.SourceFile, schema *coredb.Schema) []unreadSource {
	contributed := contributingFiles(schema)
	var out []unreadSource
	for _, src := range sources {
		if contributed[src.Path] {
			continue
		}
		out = append(out, unreadSource{Path: src.Path, Reason: reasonFor(src.Content)})
	}
	return out
}

// reasonFor classifies a source that contributed nothing. Order matters: a file
// that is not text at all must never be described by what its "comments" look
// like, because in a UTF-16 file they are not comments.
func reasonFor(content []byte) unreadReason {
	switch {
	case sourcetext.ContainsNUL(content):
		return reasonNotText
	case !declaresSomething(content):
		return reasonNoDeclarations
	default:
		return reasonNothingRecognized
	}
}

// contributingFiles is the set of source paths the schema carries a position
// from — every element and every honest-failure record, so that a file is
// counted as read the moment ANY trace of it survives into the model.
func contributingFiles(s *coredb.Schema) map[string]bool {
	seen := map[string]bool{}
	if s == nil {
		return seen
	}
	mark := func(p coredb.Pos) {
		if p.File != "" {
			seen[p.File] = true
		}
	}
	for _, t := range s.Tables {
		mark(t.Pos)
		for _, c := range t.Columns {
			mark(c.Pos)
		}
		for _, fk := range t.ForeignKeys {
			mark(fk.Pos)
		}
		for _, idx := range t.Indexes {
			mark(idx.Pos)
		}
		for _, u := range t.Unreduced {
			mark(u.Pos)
		}
	}
	for _, v := range s.Views {
		mark(v.Pos)
	}
	for _, p := range s.Procedures {
		mark(p.Pos)
	}
	for _, tr := range s.Triggers {
		mark(tr.Pos)
	}
	for _, u := range s.Unreduced {
		mark(u.Pos)
	}
	for _, w := range s.Withheld {
		mark(w.Pos)
	}
	return seen
}

// declaresSomething reports whether anything survives once whitespace and
// comments are removed.
//
// DECLARED LIMIT — the comment syntaxes are a UNION over the schema languages
// codefit reads (SQL's "--" and "/* */", MySQL's "#", Prisma's "//"), not a
// per-dialect tokenizer. Getting a dialect wrong here cannot hide anything: a
// misclassified file is still REPORTED, it merely lands under
// reasonNoDeclarations instead of reasonNothingRecognized. That is the whole
// reason the benign case is reported at all rather than skipped — skipping it
// would turn every imprecision in this function back into the silence the file
// exists to end.
//
// It is not a tokenizer and must not become one: a "--" inside a string
// literal reads as a comment opener here. That mislabels; it never silences.
func declaresSomething(content []byte) bool {
	s := string(content)
	var out strings.Builder
	for i := 0; i < len(s); {
		switch {
		case strings.HasPrefix(s[i:], "/*"):
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				i = len(s)
				continue
			}
			i += 2 + end + 2
		case strings.HasPrefix(s[i:], "--"), strings.HasPrefix(s[i:], "//"), s[i] == '#':
			for i < len(s) && s[i] != '\n' {
				i++
			}
		default:
			out.WriteByte(s[i])
			i++
		}
	}
	return strings.TrimSpace(out.String()) != ""
}

// unreadNote is the per-scan trace of the schema files codefit read NOTHING
// from — the fourth independent producer on Result.Note's shared channel, and
// the one that must be read FIRST, because it changes what every trace after it
// means: an inventory of unproven tables says nothing useful about a file that
// never reached the parser at all.
//
// It obeys the same three disciplines as the measurement inventory and the
// withholding trace (design SS7a, ADR 0034 §2.8, ADR 0043 §2.6): aggregate by
// REASON so a migration directory of 200 unreadable files is one line and not
// 200, state the FACT and its CONSEQUENCE rather than a parser diagnosis, and
// stay EMPTY when there is nothing to say.
//
// The consequence sentence is the point of the whole change. Without it a
// reader sees "0 tables, 0 findings" and cannot tell whether codefit read the
// schema and found it clean, or never read it at all.
func unreadNote(unread []unreadSource, total int) string {
	if len(unread) == 0 {
		return ""
	}
	byReason := map[unreadReason][]string{}
	var reasonOrder []unreadReason
	for _, u := range unread {
		if _, seen := byReason[u.Reason]; !seen {
			reasonOrder = append(reasonOrder, u.Reason)
		}
		byReason[u.Reason] = append(byReason[u.Reason], u.Path)
	}
	// Defect reasons first: a reader who stops after one sentence must get the
	// one that matters.
	sort.SliceStable(reasonOrder, func(i, j int) bool {
		return reasonOrder[i].defect() && !reasonOrder[j].defect()
	})

	var parts []string
	for i, reason := range reasonOrder {
		if i >= completenessInventoryReasonCap {
			parts = append(parts, fmt.Sprintf("(+%d more reasons)", len(reasonOrder)-i))
			break
		}
		paths := byReason[reason]
		shown, suffix := paths, ""
		if len(paths) > completenessInventoryTableCap {
			shown = paths[:completenessInventoryTableCap]
			suffix = fmt.Sprintf(" (+%d more)", len(paths)-completenessInventoryTableCap)
		}
		if !reason.defect() {
			parts = append(parts, fmt.Sprintf(
				"%d of the %d configured schema file(s) %s: %s%s. That is not an error — it is recorded "+
					"so that a schema with no tables is never ambiguous.",
				len(paths), total, reason, strings.Join(shown, ", "), suffix,
			))
			continue
		}
		parts = append(parts, fmt.Sprintf(
			"codefit read NOTHING from %d of the %d configured schema file(s) — %s: %s%s. "+
				"Not one statement in them reached any rule, so this scan is not a clean bill of health for them: "+
				"whatever they declare, codefit did not see it.",
			len(paths), total, reason, strings.Join(shown, ", "), suffix,
		))
	}
	return strings.Join(parts, " ")
}

// wholeScanBlind reports whether EVERY configured source went unread for a
// defect reason — the case where Measured must be false.
//
// Result.Measured already carries exactly this doctrine ("Measured=false with a
// Note is the honest 'not audited' state ... distinct from 'audited, 0
// findings'"); it had simply never been reachable from a file codefit failed to
// read. A PARTIAL failure keeps Measured=true, because codefit did audit the
// sources it could read and the note names the ones it could not — reporting
// the whole scan as unmeasured there would throw away real findings.
func wholeScanBlind(unread []unreadSource, total int) bool {
	if total == 0 || len(unread) != total {
		return false
	}
	for _, u := range unread {
		if u.Reason.defect() {
			return true
		}
	}
	return false
}
