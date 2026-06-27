package mcp

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/codefit-cli/codefit/internal/core/baseline"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/report"
)

// BaselineDelta is scan-all's account of how the current scan compares to the
// committed baseline. The agent acts on new + changed (and unaccepted
// affirmations); known surface is silenced but counted.
type BaselineDelta struct {
	New               int        `json:"new"`
	Changed           int        `json:"changed"`
	Known             int        `json:"known"`
	Acknowledged      int        `json:"acknowledged"`
	Gone              int        `json:"gone"`
	AffirmationsShown int        `json:"affirmations_shown"`
	GoneCandidates    []GoneItem `json:"gone_candidates,omitempty"`
	Note              string     `json:"note"`
}

// GoneItem names a baseline item no longer present in the code — a prune candidate.
type GoneItem struct {
	Fingerprint string `json:"fingerprint"`
	Category    string `json:"category"`
	File        string `json:"file"`
	Snippet     string `json:"snippet,omitempty"`
}

// applyBaseline loads the project's baseline, diffs it against this scan, persists
// the updated baseline, annotates each concern with its delta, and returns the
// delta plus only the endpoints that have something to show (new/changed surface,
// or an unaccepted deterministic affirmation). It never edits code — only the
// baseline file.
func applyBaseline(root string, res findings.SensorResult, endpoints []report.EndpointReport) (BaselineDelta, []report.EndpointReport, error) {
	path := filepath.Join(root, baseline.Name)
	prev, err := baseline.Load(path)
	if err != nil {
		return BaselineDelta{}, nil, fmt.Errorf("loading baseline: %w", err)
	}
	diff := baseline.Diff(prev, observedFrom(res))
	if err := diff.Next.Save(path); err != nil {
		return BaselineDelta{}, nil, fmt.Errorf("saving baseline: %w", err)
	}

	var shown []report.EndpointReport
	for _, ep := range endpoints {
		keep := false
		for i := range ep.Concerns {
			fp := ep.Concerns[i].Fingerprint
			if st, ok := diff.State[fp]; ok {
				ep.Concerns[i].Baseline = string(st)
			}
			if diff.Shown[fp] {
				keep = true
			}
		}
		if keep {
			shown = append(shown, ep)
		}
	}

	delta := BaselineDelta{
		New: diff.Counts.New, Changed: diff.Counts.Changed, Known: diff.Counts.Known,
		Acknowledged: diff.Counts.Acknowledged, Gone: diff.Counts.Gone,
		AffirmationsShown: diff.Counts.AffirmationsShown,
		GoneCandidates:    goneItems(diff.Gone),
		Note:              baselineNote(diff.Counts),
	}
	return delta, shown, nil
}

// observedFrom turns a scan result into baseline observations: deterministic
// findings are affirmations (Affirms=true, displayed by Title — never the matched
// secret), surface items are questions (Affirms=false). Deduplicated by fingerprint.
func observedFrom(res findings.SensorResult) []baseline.Observed {
	seen := map[string]bool{}
	var obs []baseline.Observed
	add := func(o baseline.Observed) {
		if o.FP == "" || seen[o.FP] {
			return
		}
		seen[o.FP] = true
		obs = append(obs, o)
	}
	for _, f := range res.Findings {
		add(baseline.Observed{FP: f.Fingerprint, Category: string(f.Dimension), File: f.File, Snippet: f.Title, Affirms: !f.Probabilistic})
	}
	for _, it := range res.Surface {
		add(baseline.Observed{FP: it.Fingerprint, Category: it.Category, File: it.File, Snippet: firstLine(it.Snippet), Affirms: false})
	}
	return obs
}

func goneItems(items []baseline.Item) []GoneItem {
	out := make([]GoneItem, 0, len(items))
	for _, it := range items {
		out = append(out, GoneItem{Fingerprint: it.FP, Category: it.Category, File: it.File, Snippet: it.Snippet})
	}
	return out
}

func baselineNote(c baseline.Counts) string {
	note := fmt.Sprintf("%d new, %d changed, %d known, %d acknowledged, %d gone",
		c.New, c.Changed, c.Known, c.Acknowledged, c.Gone)
	if c.AffirmationsShown > 0 {
		note += fmt.Sprintf("; %d deterministic affirmation(s) shown until accepted", c.AffirmationsShown)
	}
	if c.Gone > 0 {
		note += "; gone items are prune candidates"
	}
	return note
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// BaselineAcceptRequest marks baseline items as acknowledged by a human (a false
// positive or accepted debt). reason is mandatory. SAFETY: the agent must call
// this ONLY when the human decided so — codefit records by:"human" but cannot
// verify it; the skill enforces the discipline.
type BaselineAcceptRequest struct {
	Root         string   `json:"root"`
	Fingerprints []string `json:"fingerprints"`
	Reason       string   `json:"reason"`
}

// BaselineAcceptResponse reports which fingerprints were acknowledged.
type BaselineAcceptResponse struct {
	Accepted []string `json:"accepted"`
	Note     string   `json:"note"`
}

// HandleBaselineAccept records a human's decision to accept items. It only touches
// the baseline file.
func HandleBaselineAccept(req BaselineAcceptRequest) (BaselineAcceptResponse, error) {
	path := filepath.Join(req.Root, baseline.Name)
	b, err := baseline.Load(path)
	if err != nil {
		return BaselineAcceptResponse{}, fmt.Errorf("loading baseline: %w", err)
	}
	accepted, err := b.Accept(req.Fingerprints, req.Reason, time.Now().UTC().Format("2006-01-02"))
	if err != nil {
		return BaselineAcceptResponse{}, err
	}
	if err := b.Save(path); err != nil {
		return BaselineAcceptResponse{}, fmt.Errorf("saving baseline: %w", err)
	}
	return BaselineAcceptResponse{
		Accepted: accepted,
		Note:     fmt.Sprintf("Acknowledged %d item(s) with reason %q (recorded as a human decision).", len(accepted), req.Reason),
	}, nil
}

// BaselinePruneRequest removes baseline items that no longer exist in the code
// (gone). With Fingerprints set it prunes only those (if confirmed gone); empty
// prunes all confirmed-gone items.
type BaselinePruneRequest struct {
	Root         string   `json:"root"`
	Language     string   `json:"language"`
	Fingerprints []string `json:"fingerprints,omitempty"`
}

// BaselinePruneResponse reports which fingerprints were pruned.
type BaselinePruneResponse struct {
	Pruned []string `json:"pruned"`
	Note   string   `json:"note"`
}

// HandleBaselinePrune re-scans to confirm which baseline items are gone, then
// removes them. Stateless: it recomputes the current surface; it never edits code.
func HandleBaselinePrune(req BaselinePruneRequest) (BaselinePruneResponse, error) {
	res, err := runSecurity(req.Root, req.Language)
	if err != nil {
		return BaselinePruneResponse{}, err
	}
	observed := map[string]bool{}
	for _, o := range observedFrom(res) {
		observed[o.FP] = true
	}
	path := filepath.Join(req.Root, baseline.Name)
	b, err := baseline.Load(path)
	if err != nil {
		return BaselinePruneResponse{}, fmt.Errorf("loading baseline: %w", err)
	}

	wanted := map[string]bool{}
	for _, fp := range req.Fingerprints {
		wanted[fp] = true
	}
	var target []string
	for _, it := range b.Items {
		if observed[it.FP] {
			continue // still present in the code — not a prune candidate
		}
		if len(wanted) == 0 || wanted[it.FP] {
			target = append(target, it.FP)
		}
	}

	pruned := b.Prune(target)
	if err := b.Save(path); err != nil {
		return BaselinePruneResponse{}, fmt.Errorf("saving baseline: %w", err)
	}
	return BaselinePruneResponse{
		Pruned: pruned,
		Note:   fmt.Sprintf("Pruned %d resolved item(s) from the baseline.", len(pruned)),
	}, nil
}
