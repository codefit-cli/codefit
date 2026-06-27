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
//
// codefit never edits code — only this file.
package baseline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Name is the baseline file, committed at the repo root.
const Name = ".codefit-baseline"

// Ack records that a human accepted an item (false positive or accepted debt).
type Ack struct {
	Reason string `yaml:"reason"`
	At     string `yaml:"at"`
	By     string `yaml:"by"` // always "human": codefit never acknowledges on its own
}

// Item is one tracked surface item or deterministic finding. Snippet is a
// human-readable display only; it is never the matched secret (the identity is the
// content-hashed FP). Ack is non-nil only when the item was accepted.
type Item struct {
	FP       string `yaml:"fp"`
	Category string `yaml:"category"`
	File     string `yaml:"file"`
	Snippet  string `yaml:"snippet,omitempty"`
	Ack      *Ack   `yaml:"acknowledged,omitempty"`
}

// Baseline is the committed set of known items.
type Baseline struct {
	Version string `yaml:"version"`
	Items   []Item `yaml:"items"`
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
		"# tools (scan-all / baseline-accept / baseline-prune), not edited by hand.\n"
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
func Diff(prev *Baseline, observed []Observed) DiffResult {
	prevByFP := prev.index()
	observedFP := make(map[string]bool, len(observed))
	for _, o := range observed {
		observedFP[o.FP] = true
	}

	res := DiffResult{State: map[string]State{}, Shown: map[string]bool{}, Next: &Baseline{Version: "1"}}

	// Gone = previous items not observed now (prune candidates). Index them by
	// (file, category) so a new item at the same spot can be paired as "changed".
	goneByLoc := map[string][]Item{}
	var gone []Item
	for _, it := range prev.Items {
		if !observedFP[it.FP] {
			gone = append(gone, it)
			loc := it.File + "\x00" + it.Category
			goneByLoc[loc] = append(goneByLoc[loc], it)
		}
	}

	replaced := map[string]bool{} // gone fps consumed by a "changed" pairing

	for _, o := range observed {
		if prevItem, ok := prevByFP[o.FP]; ok {
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
