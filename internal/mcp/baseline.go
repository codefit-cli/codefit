package mcp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/codefit-cli/codefit/internal/core/baseline"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/report"
	"github.com/codefit-cli/codefit/internal/core/scope"
	"github.com/codefit-cli/codefit/internal/sensors"
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

// diffBaseline computes the UNIFIED baseline diff over the observed union (across
// the sensors that ran), persists the next baseline once, and builds the delta.
// It is scoped by the categories of the sensors that ran (ADR 0019), so a sensor
// that did not run never marks another dimension's items gone. Presentation
// (endpoints for security, a flat section for db) is layered on top of the same
// diff — the seam declared in ADR 0019, now used by two consumers.
// files is the pass's FILE scope: with a partial scan, a baseline item in a file
// this pass never opened must not become a gone/prune candidate (R5 of the
// change-scope spec). A full scan passes scope.Full() and the guard is inert.
func diffBaseline(prev *baseline.Baseline, path string, observed []baseline.Observed, scanned map[string]bool, files scope.Scope) (baseline.DiffResult, BaselineDelta, error) {
	diff := baseline.Diff(prev, observed, scanned, files)
	if err := diff.Next.Save(path); err != nil {
		return baseline.DiffResult{}, BaselineDelta{}, fmt.Errorf("saving baseline: %w", err)
	}
	delta := BaselineDelta{
		New: diff.Counts.New, Changed: diff.Counts.Changed, Known: diff.Counts.Known,
		Acknowledged: diff.Counts.Acknowledged, Gone: diff.Counts.Gone,
		AffirmationsShown: diff.Counts.AffirmationsShown,
		GoneCandidates:    goneItems(diff.Gone),
		Note:              baselineNote(diff.Counts),
	}
	return diff, delta, nil
}

// filterEndpointsByBaseline is security's presentation: keep the endpoints with a
// shown concern and annotate each concern's baseline state. Unchanged from before.
func filterEndpointsByBaseline(endpoints []report.EndpointReport, diff baseline.DiffResult) []report.EndpointReport {
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
	return shown
}

// filterDBByBaseline is the db dimension's NON-endpoint presentation: keep the
// findings and surface the baseline shows (new/changed surface, and unaccepted
// affirmations), filtered by diff.Shown directly (the ADR 0019 seam iii).
func filterDBByBaseline(res findings.SensorResult, diff baseline.DiffResult) ([]findings.Finding, []findings.SurfaceItem) {
	var fs []findings.Finding
	for _, f := range res.Findings {
		if diff.Shown[f.Fingerprint] {
			fs = append(fs, f)
		}
	}
	var surf []findings.SurfaceItem
	for _, it := range res.Surface {
		if diff.Shown[it.Fingerprint] {
			surf = append(surf, it)
		}
	}
	return fs, surf
}

// observedFrom turns a scan result into baseline observations: deterministic
// findings are affirmations (Affirms=true, displayed by Title — never the matched
// secret), surface items are questions (Affirms=false). Deduplicated by fingerprint.
func observedFrom(results ...findings.SensorResult) []baseline.Observed {
	seen := map[string]bool{}
	var obs []baseline.Observed
	add := func(o baseline.Observed) {
		if o.FP == "" || seen[o.FP] {
			return
		}
		seen[o.FP] = true
		obs = append(obs, o)
	}
	for _, res := range results {
		for _, f := range res.Findings {
			add(baseline.Observed{FP: f.Fingerprint, Category: string(f.Dimension), File: f.File, Snippet: f.Title, Affirms: !f.Probabilistic})
		}
		for _, it := range res.Surface {
			add(baseline.Observed{FP: it.Fingerprint, Category: it.Category, File: it.File, Snippet: firstLine(it.Snippet), Affirms: false})
		}
	}
	return obs
}

// scannedCategories unions the OwnedCategories of the sensors that ran this pass —
// the scope for the unified baseline diff/prune (ADR 0019). The MCP adapter only
// unions; each sensor declares its own categories, so a new sensor is scoped
// automatically without touching this code.
func scannedCategories(ss ...sensors.Sensor) map[string]bool {
	categories := map[string]bool{}
	for _, s := range ss {
		for _, c := range s.OwnedCategories() {
			categories[c] = true
		}
	}
	return categories
}

// recognizedHelpers loads the project's registered authz helpers for a language,
// best-effort: a missing or broken baseline must never break a scan, so it returns
// nil on error (the scan proceeds with the built-in helper set only).
func recognizedHelpers(root, language string) []string {
	b, err := baseline.Load(filepath.Join(root, baseline.Name))
	if err != nil {
		return nil
	}
	return b.RecognizedAuthzHelpers(language)
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

// BaselineListRequest reads the current baseline so the agent can reference items
// in accept/prune WITHOUT reading the raw .codefit-baseline file. Filter is "" (all),
// "known" (not yet accepted), or "acknowledged". Language is accepted for tool
// uniformity but unused — listing reads the file, it does not scan code.
type BaselineListRequest struct {
	Root     string `json:"root"`
	Language string `json:"language,omitempty"`
	Filter   string `json:"filter,omitempty"`
}

// BaselineListResponse is the projected baseline: per item just fp+file+category+
// state (+reason/date if acknowledged), small enough not to truncate. AuthzHelpers
// lists the project's registered custom authz helpers (read-only visibility) so the
// agent can see — and propose unregistering — them without reading the file.
type BaselineListResponse struct {
	Items        []baseline.Entry       `json:"items"`
	Count        int                    `json:"count"`
	AuthzHelpers []baseline.AuthzHelper `json:"authz_helpers,omitempty"`
	Note         string                 `json:"note"`
}

// HandleBaselineList returns the baseline entries. A missing baseline is NOT an
// error: it returns an empty list with a note pointing to scan-all. Read-only.
func HandleBaselineList(req BaselineListRequest) (BaselineListResponse, error) {
	path := filepath.Join(req.Root, baseline.Name)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return BaselineListResponse{Items: []baseline.Entry{}, Note: "no baseline yet — run codefit-scan-all first"}, nil
	}
	b, err := baseline.Load(path)
	if err != nil {
		return BaselineListResponse{}, fmt.Errorf("loading baseline: %w", err)
	}
	entries, err := b.List(req.Filter)
	if err != nil {
		return BaselineListResponse{}, err
	}
	note := fmt.Sprintf("%d item(s)", len(entries))
	if req.Filter != "" {
		note += " (filter: " + req.Filter + ")"
	}
	if len(b.AuthzHelpers) > 0 {
		note += fmt.Sprintf("; %d registered authz helper(s)", len(b.AuthzHelpers))
	}
	return BaselineListResponse{Items: entries, Count: len(entries), AuthzHelpers: b.AuthzHelpers, Note: note}, nil
}

// BaselineRegisterAuthzHelperRequest registers a project-specific authz helper so
// codefit recognizes it on later scans (known_authz_detected reflects it). reason
// is mandatory. SAFETY: registering silences the AUTHZ gap on EVERY item that calls
// the helper — far more reach than accepting one item. The agent must call this
// ONLY when the human approved it; codefit records by:"human" but cannot verify it
// (the skill enforces the discipline). It does NOT clear the IDOR/ownership gap
// (ADR 0013, ADR 0006 amended).
type BaselineRegisterAuthzHelperRequest struct {
	Root       string `json:"root"`
	Language   string `json:"language"`
	HelperName string `json:"helper_name"`
	Reason     string `json:"reason"`
}

// BaselineRegisterAuthzHelperResponse reports the outcome.
type BaselineRegisterAuthzHelperResponse struct {
	Registered bool   `json:"registered"`
	Note       string `json:"note"`
}

// HandleBaselineRegisterAuthzHelper records a human-approved custom authz helper in
// the baseline. It only touches the baseline file.
func HandleBaselineRegisterAuthzHelper(req BaselineRegisterAuthzHelperRequest) (BaselineRegisterAuthzHelperResponse, error) {
	path := filepath.Join(req.Root, baseline.Name)
	b, err := baseline.Load(path)
	if err != nil {
		return BaselineRegisterAuthzHelperResponse{}, fmt.Errorf("loading baseline: %w", err)
	}
	added, err := b.RegisterAuthzHelper(req.HelperName, req.Language, req.Reason, time.Now().UTC().Format("2006-01-02"))
	if err != nil {
		return BaselineRegisterAuthzHelperResponse{}, err
	}
	if !added {
		return BaselineRegisterAuthzHelperResponse{
			Registered: false,
			Note:       fmt.Sprintf("%q was already a registered authz helper for %s — no change.", req.HelperName, req.Language),
		}, nil
	}
	if err := b.Save(path); err != nil {
		return BaselineRegisterAuthzHelperResponse{}, fmt.Errorf("saving baseline: %w", err)
	}
	return BaselineRegisterAuthzHelperResponse{
		Registered: true,
		Note: fmt.Sprintf("Registered %q as a project authz helper for %s (human decision). Re-run "+
			"codefit-scan-all so items using it reflect known_authz_detected=true — the AUTHZ gap clears; "+
			"an IDOR/ownership gap on the same endpoint stays actionable.", req.HelperName, req.Language),
	}, nil
}

// BaselineUnregisterAuthzHelperRequest reverses a registration (the developer's
// decision is always reversible). The next scan stops recognizing the helper.
type BaselineUnregisterAuthzHelperRequest struct {
	Root       string `json:"root"`
	Language   string `json:"language"`
	HelperName string `json:"helper_name"`
}

// BaselineUnregisterAuthzHelperResponse reports the outcome.
type BaselineUnregisterAuthzHelperResponse struct {
	Unregistered bool   `json:"unregistered"`
	Note         string `json:"note"`
}

// HandleBaselineUnregisterAuthzHelper removes a registered helper. It only touches
// the baseline file.
func HandleBaselineUnregisterAuthzHelper(req BaselineUnregisterAuthzHelperRequest) (BaselineUnregisterAuthzHelperResponse, error) {
	path := filepath.Join(req.Root, baseline.Name)
	b, err := baseline.Load(path)
	if err != nil {
		return BaselineUnregisterAuthzHelperResponse{}, fmt.Errorf("loading baseline: %w", err)
	}
	removed := b.UnregisterAuthzHelper(req.HelperName, req.Language)
	if !removed {
		return BaselineUnregisterAuthzHelperResponse{
			Unregistered: false,
			Note:         fmt.Sprintf("%q was not a registered authz helper for %s — nothing to remove.", req.HelperName, req.Language),
		}, nil
	}
	if err := b.Save(path); err != nil {
		return BaselineUnregisterAuthzHelperResponse{}, fmt.Errorf("saving baseline: %w", err)
	}
	return BaselineUnregisterAuthzHelperResponse{
		Unregistered: true,
		Note: fmt.Sprintf("Unregistered %q for %s. Re-run codefit-scan-all; items using it will show "+
			"known_authz_detected=false again.", req.HelperName, req.Language),
	}, nil
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
	// Prune compares fingerprints (category+file+snippet), which do not depend on
	// the recognized authz helpers — so the built-in set is enough here.
	// The prune re-scan is ALWAYS full: codefit-baseline-prune accepts no scope
	// (R5 of the change-scope spec). Scanning may be cheap and partial; forgetting
	// may not — deleting audit memory requires having looked at everything.
	res, err := runSecurity(req.Root, req.Language, nil, scope.Full())
	if err != nil {
		return BaselinePruneResponse{}, err
	}
	observed := map[string]bool{}
	for _, o := range observedFrom(res) {
		observed[o.FP] = true
	}
	// Scope the prune to the categories the re-scan actually covered (security
	// here). An item owned by a sensor that did not run is NOT observed, but that
	// does not make it gone — it must never be pruned by this run (ADR 0019).
	scanned := securityScope(req.Language)
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
		if !scanned[it.Category] {
			continue // out of scope: a sensor that did not run — never prune
		}
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
