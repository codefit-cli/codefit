// Package baseline tracks codefit's view of a project's audited surface across
// scans. The baseline is a committed file (.codefit-baseline) — shared knowledge
// like .codefit.yaml — that records every item codefit knows about, identified by
// a content Fingerprint (see findings.Fingerprint), so a re-scan can tell what is
// new, unchanged (known), changed, or gone.
//
// Safeguard by certainty (PRD RF-08):
//   - SURFACE (a question) becomes known automatically once recorded; a re-scan
//     silences it. accept marks it acknowledged (a false positive / accepted debt).
//   - DETERMINISTIC (an affirmation, confidence 1.0) is NEVER known automatically:
//     it is shown on every scan until a human accepts it explicitly with a reason.
//     Silencing an affirmation is graver than silencing a question, so it needs the
//     stronger safeguard.
//   - AGENT VERDICT (an agent's reasoning about an item, confidence < 1.0) is
//     RECORDED but never ACCEPTS on its own (D1, ADR 0081): it persists in
//     Item.AgentVerdicts, always by:"agent", and leaves the item's safeguard
//     exactly where the two rules above already put it — a surface item still
//     goes known, an affirmation still shows until accepted. Recording moves
//     nothing in either direction; only a human accepts, through the same
//     Accept path as any other item.
//
// codefit never edits code — only this file.
package baseline

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/scope"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// Name is the baseline file, committed at the repo root.
const Name = ".codefit-baseline"

// Actor is who recorded an entry in this file. Exactly two values, and the
// asymmetry between them IS the safeguard (D4, ADR 0081): a human's record is a
// DECISION and can silence an item (Ack, AuthzHelper); an agent's is a
// RECOMMENDATION (AgentVerdict) and never can.
type Actor string

const (
	ActorHuman Actor = "human" // Ack, AuthzHelper — silences
	ActorAgent Actor = "agent" // AgentVerdict only — never silences
)

// Ack records that a human accepted an item (false positive or accepted debt).
type Ack struct {
	Reason string `yaml:"reason"`
	At     string `yaml:"at"`
	By     Actor  `yaml:"by"` // always "human": codefit never acknowledges on its own
}

// AgentVerdict is what an AGENT concluded after reasoning over this item's
// surface (codefit-baseline-record-verdict). Recording one NEVER silences the
// item (D1) and NEVER overwrites a previous verdict on the same fp — conflicting
// verdicts are both kept for a human to resolve (D2, see Item.InConflict). By is
// always ActorAgent, stamped by RecordVerdict itself, never caller-supplied — a
// claim scoped to THIS type; the human-only claims on Ack.By and AuthzHelper.By
// are unchanged and still true.
type AgentVerdict struct {
	Verdict    surface.Verdict   `yaml:"verdict"`
	Reasoning  string            `yaml:"reasoning"`
	Confidence float64           `yaml:"confidence"`
	Severity   findings.Severity `yaml:"severity,omitempty"`
	At         string            `yaml:"at"`
	By         Actor             `yaml:"by"`
}

// Item is one tracked surface item or deterministic finding. Snippet is a
// human-readable display only; it is never the matched secret (the identity is the
// content-hashed FP). Ack is non-nil only when the item was accepted.
// AgentVerdicts is the append-only history of what an agent concluded about this
// item across audit passes — never trimmed except by ADR 0009's identity reset
// (a code edit changes FP, which starts a new item with an empty history).
type Item struct {
	FP            string         `yaml:"fp"`
	Category      string         `yaml:"category"`
	File          string         `yaml:"file"`
	Snippet       string         `yaml:"snippet,omitempty"`
	AgentVerdicts []AgentVerdict `yaml:"agent_verdicts,omitempty"`
	Ack           *Ack           `yaml:"acknowledged,omitempty"`
}

// InConflict reports whether this item's agent verdicts disagree: at least one
// "vulnerable" AND at least one "not_vulnerable" (an "uncertain" verdict
// participates in neither direction). Derived, never stored — every reader
// applies its own current rule to the raw facts rather than trusting a flag a
// different binary version may have written (D2).
func (it Item) InConflict() bool {
	var vuln, notVuln bool
	for _, v := range it.AgentVerdicts {
		switch v.Verdict {
		case surface.VerdictVulnerable:
			vuln = true
		case surface.VerdictNotVulnerable:
			notVuln = true
		}
	}
	return vuln && notVuln
}

// AuthzHelper is a project-specific authorization helper the AGENT identified by
// reasoning over the code and a HUMAN approved registering — so codefit recognizes
// it on later scans without the agent re-reasoning (it augments the built-in
// NextAuth-style set per project). It is project knowledge, like an acknowledged
// item: persisted, committed, recorded by:"human". Registering a helper changes a
// FACT (known_authz_detected for the authz concern), never a verdict — it clears
// the AUTHZ gap, never the IDOR/ownership gap (ADR 0013, ADR 0006 amended).
type AuthzHelper struct {
	Name     string `yaml:"name"`
	Language string `yaml:"language"`
	Reason   string `yaml:"reason"`
	At       string `yaml:"at"`
	By       Actor  `yaml:"by"` // always "human": codefit never registers on its own
}

// Baseline is the committed set of known items plus the project's registered authz
// helpers.
type Baseline struct {
	Version      string        `yaml:"version"`
	Items        []Item        `yaml:"items"`
	AuthzHelpers []AuthzHelper `yaml:"authz_helpers,omitempty"`

	// unknown holds top-level YAML keys this binary does not recognize (D6-B,
	// ADR 0081): preserved verbatim across Load->Save and Load->Diff->Save (see
	// Diff, which builds Next from scratch and must carry this forward
	// explicitly), so an OLDER codefit binary reading a NEWER baseline does not
	// silently delete a field it does not know about yet. It protects the NEXT
	// format addition; it does NOT protect the CURRENT one — v0.2.6-v0.2.9 are
	// already distributed without this guard (D6-A, accepted and declared).
	unknown map[string]yaml.Node `yaml:"-"`
}

// UnmarshalYAML decodes the known Baseline fields normally, then captures any
// top-level key this binary does not recognize (D6-B) so Save can re-emit it
// unchanged.
func (b *Baseline) UnmarshalYAML(value *yaml.Node) error {
	type plain Baseline
	var p plain
	if err := value.Decode(&p); err != nil {
		return err
	}
	*b = Baseline(p)

	var raw map[string]yaml.Node
	if err := value.Decode(&raw); err != nil {
		return err
	}
	for _, known := range []string{"version", "items", "authz_helpers"} {
		delete(raw, known)
	}
	if len(raw) > 0 {
		b.unknown = raw
	}
	return nil
}

// MarshalYAML encodes the known Baseline fields normally, then re-emits any
// unknown top-level key this binary preserved from Load (D6-B), sorted for
// deterministic output.
func (b Baseline) MarshalYAML() (interface{}, error) {
	type plain Baseline
	node := &yaml.Node{}
	if err := node.Encode(plain(b)); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(b.unknown))
	for k := range b.unknown {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := b.unknown[k]
		node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, &v)
	}
	return node, nil
}

// Observed is one item seen in the current scan. Affirms is true for a
// deterministic finding (an affirmation) and false for a surface item (a question).
type Observed struct {
	FP       string
	Category string
	File     string
	Snippet  string
	Affirms  bool
}

// State is an item's delta against the previous baseline.
type State string

const (
	StateNew     State = "new"
	StateChanged State = "changed"
	StateKnown   State = "known"
	StateAcked   State = "acknowledged"
)

// Counts is the at-a-glance delta.
type Counts struct {
	New, Changed, Known, Acknowledged, Gone, AffirmationsShown int
}

// DiffResult is the full comparison of a scan against the previous baseline.
type DiffResult struct {
	State  map[string]State // fp → state, for observed items
	Shown  map[string]bool  // fp → must be shown (not silenced)
	Gone   []Item           // baseline items no longer observed (prune candidates)
	Counts Counts
	Next   *Baseline // the baseline to persist
}

// Load reads the baseline at path. A missing file is NOT an error: it returns an
// empty baseline (the first scan creates it).
func Load(path string) (*Baseline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Baseline{Version: "1"}, nil
		}
		return nil, fmt.Errorf("reading baseline %q: %w", path, err)
	}
	var b Baseline
	if err := yaml.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("parsing baseline %q: %w", path, err)
	}
	if b.Version == "" {
		b.Version = "1"
	}
	return &b, nil
}

// Save writes the baseline to path as commented, human-readable YAML.
func (b *Baseline) Save(path string) error {
	if b.Version == "" {
		b.Version = "1"
	}
	body, err := yaml.Marshal(b)
	if err != nil {
		return fmt.Errorf("encoding baseline: %w", err)
	}
	header := "# .codefit-baseline — codefit's view of the audited surface.\n" +
		"# Committed (shared knowledge, like .codefit.yaml). Managed by codefit's MCP\n" +
		"# tools (scan-all / baseline-accept / baseline-prune / baseline-record-verdict),\n" +
		"# not edited by hand. WARNING (ADR 0081): an older codefit binary does not know\n" +
		"# about agent_verdicts (or any field newer than its own version) and silently\n" +
		"# drops it if it re-saves this file — a recoverable, visible-in-review loss via\n" +
		"# `git diff`/`git revert`, not a silent one, but real across mixed binary\n" +
		"# versions on the same repo.\n"
	// Atomic replace: write to a temp file in the same dir, then rename. Avoids a
	// torn file on crash and a last-writer-wins read-modify-write race between
	// concurrent scan-all/accept/prune calls.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".codefit-baseline-*.tmp")
	if err != nil {
		return fmt.Errorf("creating temp baseline: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op after a successful rename
	if _, err := tmp.Write(append([]byte(header), body...)); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing temp baseline: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp baseline: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replacing baseline %q: %w", path, err)
	}
	return nil
}

func (b *Baseline) index() map[string]Item {
	m := make(map[string]Item, len(b.Items))
	for _, it := range b.Items {
		m[it.FP] = it
	}
	return m
}

// Diff compares the observed scan against the previous baseline and returns the
// per-item delta, what must be shown, the gone candidates, and the next baseline
// to persist. It never silences a deterministic affirmation that has not been
// acknowledged.
//
// The scope is TWO-DIMENSIONAL, and an item is eligible to be "gone" only when
// BOTH dimensions admit it:
//
//	scanned[item.Category]  AND  files.Includes(item.File)
//
// scanned is the set of Item categories owned by the sensors that RAN this pass.
// A previous item whose category is NOT in scanned (its sensor did not run) is
// carried forward UNTOUCHED — absent from the delta, never a gone/prune
// candidate. This distinguishes "not observed because the sensor did not run"
// from "not observed because it disappeared", so a single-sensor run cannot
// corrupt another dimension's state (ADR 0019).
//
// files is the FILE scope of the pass. The category dimension alone does not
// cover a partial audit: a security finding in a file this pass never opened
// still belongs to the security category, which DID run, so a category-only
// guard would see it unobserved-and-in-scope and mark it gone — and
// codefit-baseline-prune would then delete the audit memory of every file the
// scan did not look at, silently, in the direction of going blind.
//
// Both dimensions fail safe: an empty scanned and the zero-value scope.Scope
// each include nothing, so a caller that forgets one under-reports and never
// prunes. Under-report, never corrupt.
func Diff(prev *Baseline, observed []Observed, scanned map[string]bool, files scope.Scope) DiffResult {
	prevByFP := prev.index()
	observedFP := make(map[string]bool, len(observed))
	for _, o := range observed {
		observedFP[o.FP] = true
	}

	// Next carries forward the registered authz helpers: they are project knowledge,
	// not per-scan observations, so a scan must never drop them. It also carries
	// forward any unrecognized top-level field (D6-B) — Next is built FROM SCRATCH
	// here, so without this explicit carry the catch-all in Load/Save alone is not
	// enough: a Load->Diff->Save round trip would still lose it.
	res := DiffResult{State: map[string]State{}, Shown: map[string]bool{}, Next: &Baseline{Version: "1", AuthzHelpers: prev.AuthzHelpers, unknown: prev.unknown}}

	// Partition the previous items: out-of-scope items (their sensor did not run)
	// are carried forward verbatim and take no part in the delta. In-scope items
	// not observed now are "gone" (prune candidates), indexed by (file, category)
	// so a new item at the same spot can be paired as "changed".
	goneByLoc := map[string][]Item{}
	var gone []Item
	for _, it := range prev.Items {
		// Both dimensions must admit the item. Either one alone is a way to go
		// blind: a category-only guard prunes the files a partial pass never
		// opened, and a file-only guard prunes the dimensions whose sensor did
		// not run.
		if !scanned[it.Category] || !files.Includes(it.File) {
			res.Next.Items = append(res.Next.Items, it) // out of scope: untouched carry-forward
			continue
		}
		if !observedFP[it.FP] {
			gone = append(gone, it)
			loc := it.File + "\x00" + it.Category
			goneByLoc[loc] = append(goneByLoc[loc], it)
		}
	}

	replaced := map[string]bool{} // gone fps consumed by a "changed" pairing

	for _, o := range observed {
		if prevItem, ok := prevByFP[o.FP]; ok {
			// R2's symmetry: the SAME two-dimensional guard that decides whether a
			// previous item may become "gone" also decides whether it may be
			// promoted to "known" — an item's state may only be advanced by a pass
			// that actually looked at it. When either dimension fails, this
			// observation is not this item's confirmation: the item was already
			// carried forward verbatim by the out-of-scope loop above, so it is
			// left untouched here (never re-added — that would duplicate it in
			// Next.Items) and never marked known/shown from an observation whose
			// own category or file this pass declared out of scope. This matters
			// concretely for the code x schema cross rules (DB-010/DB-013): their
			// fingerprint anchors to the schema file, which the DB dimension
			// always reads in full, so a narrowed pass can re-observe the same
			// fingerprint from a shrunken set of query filters even though its
			// category was deliberately excluded from `scanned` (ADR 0029).
			if !scanned[prevItem.Category] || !files.Includes(prevItem.File) {
				continue
			}
			if prevItem.Ack != nil {
				res.State[o.FP] = StateAcked
				res.Counts.Acknowledged++
				// silenced
				res.Next.Items = append(res.Next.Items, refresh(prevItem, o))
				continue
			}
			res.State[o.FP] = StateKnown
			res.Counts.Known++
			if o.Affirms {
				res.Shown[o.FP] = true // affirmation: never auto-silenced
				res.Counts.AffirmationsShown++
			}
			res.Next.Items = append(res.Next.Items, refresh(prevItem, o))
			continue
		}
		// New fp. Pair with a gone item at the same (file, category) → "changed".
		loc := o.File + "\x00" + o.Category
		if pool := goneByLoc[loc]; len(pool) > 0 {
			old := pool[0]
			goneByLoc[loc] = pool[1:]
			replaced[old.FP] = true
			res.State[o.FP] = StateChanged
			res.Counts.Changed++
		} else {
			res.State[o.FP] = StateNew
			res.Counts.New++
		}
		res.Shown[o.FP] = true
		if o.Affirms {
			res.Counts.AffirmationsShown++ // a new/changed affirmation is shown too
		}
		res.Next.Items = append(res.Next.Items, Item{FP: o.FP, Category: o.Category, File: o.File, Snippet: o.Snippet})
	}

	// Unreplaced gone items linger in the baseline (and are reported) until pruned.
	for _, it := range gone {
		if replaced[it.FP] {
			continue
		}
		res.Gone = append(res.Gone, it)
		res.Counts.Gone++
		res.Next.Items = append(res.Next.Items, it)
	}
	return res
}

// refresh keeps the previous item's identity and ack, updating the human-readable
// display fields from the current observation.
func refresh(prev Item, o Observed) Item {
	prev.Category = o.Category
	prev.File = o.File
	if o.Snippet != "" {
		prev.Snippet = o.Snippet
	}
	return prev
}

// Accept marks the given fingerprints as acknowledged by a human. A reason is
// mandatory; an unknown fingerprint is an error (nothing is changed). codefit
// records by:"human" — it never acknowledges on its own.
func (b *Baseline) Accept(fps []string, reason, at string) (accepted []string, err error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, fmt.Errorf("accept requires a reason (a human's justification)")
	}
	idx := map[string]int{}
	for i, it := range b.Items {
		idx[it.FP] = i
	}
	for _, fp := range fps {
		if _, ok := idx[fp]; !ok {
			return nil, fmt.Errorf("fingerprint %q is not in the baseline", fp)
		}
	}
	for _, fp := range fps {
		b.Items[idx[fp]].Ack = &Ack{Reason: reason, At: at, By: "human"}
		accepted = append(accepted, fp)
	}
	return accepted, nil
}

// maxVerdictReasonLen bounds an AgentVerdict's Reasoning at STORAGE time,
// distinct from maxReasonLen (200 runes, LIST-time only and thus not a bound on
// disk/git growth — see the scaling discussion in ADR 0081). 500 runes is
// generous enough for a real rationale while keeping a large, reasoned baseline
// from growing unbounded per verdict.
const maxVerdictReasonLen = 500

func truncateVerdictReasoning(s string) string {
	r := []rune(s)
	if len(r) <= maxVerdictReasonLen {
		return s
	}
	return string(r[:maxVerdictReasonLen]) + "…"
}

// RecordVerdict appends an agent's reasoning about one item to the baseline,
// creating the item (fp/category/file/snippet) if this is its first record.
// It NEVER sets Ack (D1: an agent verdict never silences the item) and NEVER
// overwrites a previous verdict on the same fp — conflicting verdicts are both
// kept for a human to resolve (D2, Item.InConflict). By is stamped ActorAgent
// here, unconditionally, regardless of what the caller passed in v.By — the
// same handler-assigned discipline Accept and RegisterAuthzHelper already
// apply to their own by:"human". Reasoning is capped at maxVerdictReasonLen
// runes, here in core, so every caller is bounded.
func (b *Baseline) RecordVerdict(fp, category, file, snippet string, v AgentVerdict) *Item {
	v.By = ActorAgent
	v.Reasoning = truncateVerdictReasoning(v.Reasoning)
	for i := range b.Items {
		if b.Items[i].FP == fp {
			b.Items[i].AgentVerdicts = append(b.Items[i].AgentVerdicts, v)
			return &b.Items[i]
		}
	}
	b.Items = append(b.Items, Item{FP: fp, Category: category, File: file, Snippet: snippet, AgentVerdicts: []AgentVerdict{v}})
	return &b.Items[len(b.Items)-1]
}

// Entry is a read-only projection of a baseline item for codefit-baseline-list:
// just what the agent needs to reference an item in accept/prune. It omits the
// snippet on purpose (the agent supplies the reasoning; this keeps the list small).
type Entry struct {
	Fingerprint string `json:"fingerprint"`
	File        string `json:"file"`
	Category    string `json:"category"`
	State       State  `json:"state"` // known | acknowledged
	Reason      string `json:"reason,omitempty"`
	At          string `json:"at,omitempty"`
}

// List returns the baseline items as Entries, filtered by state: "" (all),
// "known" (not acknowledged), or "acknowledged". An unknown filter is an error.
func (b *Baseline) List(filter string) ([]Entry, error) {
	switch filter {
	case "", "known", "acknowledged":
	default:
		return nil, fmt.Errorf("invalid filter %q (allowed: known, acknowledged, or empty for all)", filter)
	}
	out := make([]Entry, 0, len(b.Items))
	for _, it := range b.Items {
		acked := it.Ack != nil
		if filter == "known" && acked {
			continue
		}
		if filter == "acknowledged" && !acked {
			continue
		}
		e := Entry{Fingerprint: it.FP, File: it.File, Category: it.Category, State: StateKnown}
		if acked {
			e.State = StateAcked
			e.Reason = truncateReason(it.Ack.Reason)
			e.At = it.Ack.At
		}
		out = append(out, e)
	}
	return out, nil
}

// maxReasonLen bounds the reason in a list Entry so a baseline with many
// acknowledged items (each with a long justification) cannot grow the list
// response into MCP truncation territory (the anti-truncation principle of ADR
// 0008). The FULL reason always lives in the committed .codefit-baseline; the list
// shows enough to glance at why an item was accepted.
const maxReasonLen = 200

func truncateReason(s string) string {
	r := []rune(s)
	if len(r) <= maxReasonLen {
		return s
	}
	return string(r[:maxReasonLen]) + "…"
}

// RegisterAuthzHelper records a project-specific authz helper as recognized,
// by:"human". A name, language, and reason are mandatory (the reason is the
// human's justification, as for Accept). Idempotent: registering an already-known
// (language, name) is a no-op that returns added=false. codefit never registers on
// its own — the agent proposes, the human decides (the skill enforces it; codefit
// records the decision).
func (b *Baseline) RegisterAuthzHelper(name, language, reason, at string) (added bool, err error) {
	name = strings.TrimSpace(name)
	language = strings.TrimSpace(language)
	reason = strings.TrimSpace(reason)
	if name == "" {
		return false, fmt.Errorf("register requires a helper name")
	}
	if language == "" {
		return false, fmt.Errorf("register requires a language")
	}
	if reason == "" {
		return false, fmt.Errorf("register requires a reason (a human's justification)")
	}
	for _, h := range b.AuthzHelpers {
		if h.Language == language && h.Name == name {
			return false, nil // already recognized — idempotent
		}
	}
	b.AuthzHelpers = append(b.AuthzHelpers, AuthzHelper{Name: name, Language: language, Reason: reason, At: at, By: "human"})
	return true, nil
}

// UnregisterAuthzHelper removes a registered helper (the reversal of
// RegisterAuthzHelper — the developer's decision is always reversible). Returns
// whether a helper was removed.
func (b *Baseline) UnregisterAuthzHelper(name, language string) bool {
	name = strings.TrimSpace(name)
	language = strings.TrimSpace(language)
	kept := b.AuthzHelpers[:0]
	removed := false
	for _, h := range b.AuthzHelpers {
		if h.Name == name && h.Language == language {
			removed = true
			continue
		}
		kept = append(kept, h)
	}
	b.AuthzHelpers = kept
	return removed
}

// RecognizedAuthzHelpers returns the names of the helpers registered for a
// language — the set a scan adds to the built-in authz helpers for that project.
func (b *Baseline) RecognizedAuthzHelpers(language string) []string {
	var out []string
	for _, h := range b.AuthzHelpers {
		if h.Language == language {
			out = append(out, h.Name)
		}
	}
	return out
}

// Prune removes the given fingerprints from the baseline (used for gone items the
// caller has confirmed no longer exist in the code). Returns the removed fps.
func (b *Baseline) Prune(fps []string) (pruned []string) {
	remove := make(map[string]bool, len(fps))
	for _, fp := range fps {
		remove[fp] = true
	}
	kept := b.Items[:0]
	for _, it := range b.Items {
		if remove[it.FP] {
			pruned = append(pruned, it.FP)
			continue
		}
		kept = append(kept, it)
	}
	b.Items = kept
	return pruned
}
