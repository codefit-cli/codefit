//go:build dogfood

// Dogfood harness for the two limits the finding cache and the change scope
// still carried: both were "covered by tests and the CI self-audit but NOT
// exercised on a real project yet". Fixtures prove a contract; they cannot
// prove the contract survives 300 files of somebody's real TypeScript, and they
// cannot produce a single measured millisecond. This file does both.
//
// READ-ONLY OVER THE PROJECTS. The dogfood roots are working clones, so nothing
// here writes inside one: no .codefit.yaml, no .codefit/ directory, no temp
// file. That is precisely why these tests drive security.Sensor directly instead
// of the MCP handler — runSecurity loads .codefit.yaml FROM the project root, so
// turning the cache on through it would mean writing into the clone. Every piece
// of state lives in t.TempDir(): the config is synthesized in memory and the
// cache directory is an absolute temp path (cacheFor only joins a RELATIVE dir
// onto ProjectRoot).
//
// Build-tagged `dogfood`, like dogfood_cross_test.go: EXCLUDED from the normal
// gate. Skips clean when dogfood.local.json is absent — see that file's header
// for the format.
//
//	go test -tags dogfood -run 'TestDogfoodCache|TestDogfoodChangeScope' -v ./internal/mcp/
package mcp

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/scope"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/sensors/security"
)

// loadDogfoodProjects reads dogfood.local.json, or skips the whole test when it
// is absent — the skip-if-absent contract the cross harness established, kept in
// ONE place so the two harnesses cannot drift into disagreeing about what a
// missing config means.
func loadDogfoodProjects(t *testing.T) []dogfoodProject {
	t.Helper()
	path := dogfoodConfigPath()
	if !filepath.IsAbs(path) {
		path = filepath.Join("..", "..", path) // test cwd is the package dir; config is at repo root
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no dogfood config (skip-if-absent): %v — see dogfood_cross_test.go's header to create one", err)
	}
	var projects []dogfoodProject
	if err := json.Unmarshal(raw, &projects); err != nil {
		t.Fatalf("parsing dogfood config %s: %v", path, err)
	}
	if len(projects) == 0 {
		t.Skip("dogfood config is empty")
	}
	return projects
}

// countingProvider wraps a real provider and counts the files it analyses. It
// performs no analysis of its own. Without it a cache test compares two runs
// that both did the full work and calls the agreement a cache hit — the exact
// pass-by-vacuum this file exists to refuse. Mirrors the decorator in
// internal/sensors/security/cache_test.go; it cannot be shared, that one is
// unexported in another package.
type countingProvider struct {
	providers.LanguageProvider
	securityCalls int
	surfaceCalls  int
}

func (p *countingProvider) AnalyzeSecurity(src providers.SourceFile) ([]findings.Finding, error) {
	p.securityCalls++
	return p.LanguageProvider.AnalyzeSecurity(src)
}

func (p *countingProvider) AnalyzeSurface(src providers.SourceFile) ([]findings.SurfaceItem, error) {
	p.surfaceCalls++
	return p.LanguageProvider.AnalyzeSurface(src)
}

func (p *countingProvider) calls() int { return p.securityCalls + p.surfaceCalls }

// dogfoodProvider is the REAL TypeScript provider the MCP adapter resolves, so
// what these tests measure is what an agent gets.
func dogfoodProvider(t *testing.T) providers.LanguageProvider {
	t.Helper()
	p := providerForLanguage("typescript", nil)
	if p == nil {
		t.Fatal("no provider for \"typescript\" — providerForLanguage changed and this harness measures nothing")
	}
	return p
}

// dogfoodConfig is the in-memory .codefit.yaml. It is never written anywhere:
// the dogfood roots are working clones and stay untouched. cacheDir is absolute
// (a t.TempDir()), which is what keeps the cache out of the project — an empty
// or relative dir would be joined onto ProjectRoot by cacheFor.
func dogfoodConfig(t *testing.T, p providers.LanguageProvider, cacheDir string) *config.Config {
	t.Helper()
	if cacheDir != "" && !filepath.IsAbs(cacheDir) {
		t.Fatalf("cache dir %q is relative: it would be created INSIDE the dogfood project", cacheDir)
	}
	cfg := &config.Config{Project: config.Project{
		Language:        "typescript",
		PathCriticality: p.DefaultPathCriticality(),
	}}
	if cacheDir != "" {
		cfg.Cache = config.Cache{Enabled: true, Dir: cacheDir}
	}
	return cfg
}

func dogfoodRun(t *testing.T, root string, p providers.LanguageProvider, cfg *config.Config, scp scope.Scope) findings.SensorResult {
	t.Helper()
	res, err := security.New(p).Run(auditctx.AuditContext{
		ProjectRoot: root, Language: "typescript", Config: cfg, Scope: scp,
	})
	if err != nil {
		t.Fatalf("security sensor Run over %s: %v", root, err)
	}
	return res
}

// dogfoodRoot resolves a project's root, or skips this subtest when the clone is
// not on this machine.
func dogfoodRoot(t *testing.T, p dogfoodProject) string {
	t.Helper()
	if _, err := os.Stat(p.Root); err != nil {
		t.Skipf("root absent (skip-if-absent): %s", p.Root)
	}
	return p.Root
}

// cacheEntryFiles are the entry files the cache wrote, wherever under dir the
// generation layout put them.
func cacheEntryFiles(t *testing.T, dir string) []string {
	t.Helper()
	// A directory that was never created holds no entries. Reported as zero
	// rather than as a walk error, so the CALLER's guard gets to say what the
	// absence means instead of the harness dying with a stat message.
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil
	}
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".json") {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the cache dir %q: %v", dir, err)
	}
	return out
}

// wireResult is the result JSON a consumer sees, with the wall-clock duration
// zeroed: how long the run took is a measurement OF the run, not part of what
// the run concluded, and a cache that works necessarily changes it (the
// precedent is crosswiring_test.go's byte-identical comparison).
func wireResult(t *testing.T, res findings.SensorResult) string {
	t.Helper()
	res.DurationMs = 0
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestDogfoodCache lifts the finding cache's declared limit: cold vs warm over
// REAL projects, byte-identical, with the saving measured.
//
// Every assertion here is guarded against passing by vacuum, because that is the
// entire risk of moving a cache test off fixtures: a run over a real repository
// that quietly analysed nothing — wrong extensions, everything under a skipped
// directory, a walk that returned early — produces two identical empty results
// and "passes" while proving that nothing equals nothing.
func TestDogfoodCache(t *testing.T) {
	// Whether the corpus as a whole exercised each half. A project can honestly
	// produce neither: metricasbatch is a Vite SPA and plantalinda's only Next
	// route handler is a static healthz, so codefit's backend surface is empty
	// there BY DESIGN (ADR 0005's frontier), not by accident. Those subtests
	// still prove the walk, the store and the warm hit over hundreds of real
	// files — but they cannot prove the payload survives the round trip, so the
	// requirement is asserted across the corpus after the loop instead of being
	// faked per project.
	var measured, withFindings, withSurface int

	for _, p := range loadDogfoodProjects(t) {
		t.Run(p.Name, func(t *testing.T) {
			root := dogfoodRoot(t, p)
			base := dogfoodProvider(t)
			cacheDir := filepath.Join(t.TempDir(), "cache")
			cfg := dogfoodConfig(t, base, cacheDir)

			cold := &countingProvider{LanguageProvider: base}
			coldRes := dogfoodRun(t, root, cold, cfg, scope.Full())

			// --- vacuum guards on the cold run ---
			if len(coldRes.AuditedFiles) == 0 {
				t.Fatalf("the cold scan audited 0 files under %s — the comparison below would be "+
					"two empty results agreeing with each other", root)
			}
			measured++
			if len(coldRes.Findings) > 0 {
				withFindings++
			}
			if len(coldRes.Surface) > 0 {
				withSurface++
			}
			if len(coldRes.Findings) == 0 && len(coldRes.Surface) == 0 {
				t.Logf("NOTE %s produced neither findings nor surface: this subtest proves the walk, "+
					"the store and the warm hit over %d real files (and that an EMPTY analysis is "+
					"cached rather than recomputed), NOT that the payload survives the round trip",
					p.Name, len(coldRes.AuditedFiles))
			}
			if cold.calls() == 0 {
				t.Fatal("the cold scan analysed nothing: the provider was never called")
			}
			// The entries must actually be on disk, one per audited file. Without
			// this the warm run could be a hit-free no-op and still look identical.
			if got, want := len(cacheEntryFiles(t, cacheDir)), len(coldRes.AuditedFiles); got != want {
				t.Fatalf("the cache holds %d entr(ies) for %d audited file(s) — the cold scan did not "+
					"populate the cache, so the warm scan cannot be served from it", got, want)
			}

			warm := &countingProvider{LanguageProvider: base}
			warmRes := dogfoodRun(t, root, warm, cfg, scope.Full())

			if warm.calls() != 0 {
				t.Errorf("the warm scan re-analysed %d file(s) (security=%d surface=%d) — nothing was "+
					"served from the cache, so this test compared two cold runs",
					warm.calls(), warm.securityCalls, warm.surfaceCalls)
			}
			if got, want := wireResult(t, warmRes), wireResult(t, coldRes); got != want {
				t.Errorf("warm scan differs from cold scan on %s.\ncold: %s\nwarm: %s", p.Name, want, got)
			}

			speedup := 0.0
			if warmRes.DurationMs > 0 {
				speedup = float64(coldRes.DurationMs) / float64(warmRes.DurationMs)
			}
			t.Logf("CACHE %-14s files=%d/%d findings=%d surface=%d entries=%d | cold=%dms warm=%dms (%.1fx)",
				p.Name, len(coldRes.AuditedFiles), coldRes.AuditableTotal,
				len(coldRes.Findings), len(coldRes.Surface), len(cacheEntryFiles(t, cacheDir)),
				coldRes.DurationMs, warmRes.DurationMs, speedup)
		})
	}

	if measured == 0 {
		if t.Failed() {
			return // a subtest already failed; "nothing was present" would be a lie
		}
		t.Skip("no dogfood project was present on this machine")
	}
	// The corpus-level version of the vacuum guard: a cache that dropped findings,
	// or dropped surface, must not be able to pass this whole test. If the local
	// corpus is all empty projects, that is what fails — the measurement is not
	// entitled to say "the cache preserves the payload on real code".
	if withFindings == 0 {
		t.Errorf("not one of the %d measured project(s) produced a finding: this run cannot show that "+
			"a warm cache preserves findings — add a project that does", measured)
	}
	if withSurface == 0 {
		t.Errorf("not one of the %d measured project(s) produced surface: a findings-only cache would "+
			"pass this whole test — add a project with a backend route handler", measured)
	}
}

// dogfoodScopeN is how many real files a narrowed pass asks for. Small on
// purpose: the point is the gap between what was audited and what was auditable.
const dogfoodScopeN = 5

// pickScopeFiles chooses n files for the narrowed pass, DETERMINISTICALLY (the
// inputs are sorted, so a rerun picks the same files) and biased towards the
// files that produced something. Taking the first n alphabetically is
// reproducible but tends to land on barrel files and type declarations, which
// would make "the narrowed verdict equals the full verdict on these files" a
// comparison of two empty slices.
func pickScopeFiles(full findings.SensorResult, n int) []string {
	productive := map[string]bool{}
	for _, f := range full.Findings {
		productive[f.File] = true
	}
	for _, s := range full.Surface {
		productive[s.File] = true
	}
	picked := make([]string, 0, n)
	taken := make(map[string]bool, n)
	// Two passes over the SORTED audited list: productive files first, then
	// whatever is needed to reach n.
	for _, want := range []bool{true, false} {
		for _, f := range full.AuditedFiles {
			if len(picked) == n {
				return picked
			}
			if taken[f] || productive[f] != want {
				continue
			}
			taken[f] = true
			picked = append(picked, f)
		}
	}
	return picked
}

// filterByFile keeps the items whose file is in keep, preserving order. The
// sensor appends in walk order and filepath.WalkDir is lexical, so a filtered
// full run and a narrowed run over the same files are in the same order.
func filterByFile[T any](items []T, keep map[string]bool, file func(T) string) []T {
	var out []T
	for _, it := range items {
		if keep[file(it)] {
			out = append(out, it)
		}
	}
	return out
}

func payloadJSON(t *testing.T, fs []findings.Finding, surface []findings.SurfaceItem) string {
	t.Helper()
	b, err := json.Marshal(struct {
		Findings []findings.Finding     `json:"findings"`
		Surface  []findings.SurfaceItem `json:"surface"`
	}{fs, surface})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestDogfoodChangeScope lifts the change scope's declared limit: layer 0 over
// real code, and the honest-narrowing contract measured against a real
// denominator.
//
// The requested paths are deliberately spelled NON-canonically (a "./" prefix
// and OS separators), which is how an agent handing codefit a git diff on
// Windows actually spells them. That is what makes "nothing went unmatched" an
// assertion rather than a tautology: the paths come from the full run, so the
// only way they can fail to match is scope.Canon.
func TestDogfoodChangeScope(t *testing.T) {
	for _, p := range loadDogfoodProjects(t) {
		t.Run(p.Name, func(t *testing.T) {
			root := dogfoodRoot(t, p)
			base := dogfoodProvider(t)
			cfg := dogfoodConfig(t, base, "") // no cache: this test is about layer 0

			full := dogfoodRun(t, root, base, cfg, scope.Full())
			if len(full.AuditedFiles) == 0 {
				t.Fatalf("a full scan of %s audited 0 files — there is nothing to narrow", root)
			}
			if full.AuditableTotal != len(full.AuditedFiles) {
				t.Fatalf("a FULL scan audited %d of %d auditable files: the walk skipped files nothing "+
					"asked it to skip", len(full.AuditedFiles), full.AuditableTotal)
			}

			n := dogfoodScopeN
			if n >= len(full.AuditedFiles) {
				n = len(full.AuditedFiles) / 2
			}
			if n == 0 {
				t.Fatalf("%s has only %d auditable file(s): too few to narrow, this subtest would "+
					"compare a scope against itself", p.Name, len(full.AuditedFiles))
			}
			picked := pickScopeFiles(full, n)

			requested := make([]string, len(picked))
			for i, f := range picked {
				requested[i] = "." + string(filepath.Separator) + filepath.FromSlash(f)
			}
			scp := scope.Of(requested)
			if !scp.Narrows() {
				t.Fatalf("scope.Of(%v) does not narrow — the pass below would be a full audit and "+
					"every assertion here would be vacuous", requested)
			}

			narrow := dogfoodRun(t, root, base, cfg, scp)

			// AuditedFiles ⊆ requested.
			wanted := make(map[string]bool, len(picked))
			for _, f := range picked {
				wanted[f] = true
			}
			for _, f := range narrow.AuditedFiles {
				if !wanted[f] {
					t.Errorf("the narrowed pass audited %q, which was not in scope", f)
				}
			}
			// ...and every requested path was actually reached: nothing silently
			// unmatched. Both directions, or a pass that audited half the scope
			// and a pass that audited all of it look the same.
			audited := make(map[string]bool, len(narrow.AuditedFiles))
			for _, f := range narrow.AuditedFiles {
				audited[f] = true
			}
			for _, f := range picked {
				if !audited[f] {
					t.Errorf("requested %q was never audited — a path the full scan DID audit went "+
						"unmatched under a narrowed scope", f)
				}
			}

			// The denominator is the whole project, not the scope: "5 of 412", never
			// the self-flattering "5 of 5".
			if narrow.AuditableTotal != full.AuditableTotal {
				t.Errorf("narrowed AuditableTotal = %d, want %d (the whole project) — the narrowing "+
					"shrank its own denominator", narrow.AuditableTotal, full.AuditableTotal)
			}
			if narrow.AuditableTotal <= len(narrow.AuditedFiles) {
				t.Errorf("narrowed pass reports %d audited of %d auditable: the denominator collapsed "+
					"onto the scope", len(narrow.AuditedFiles), narrow.AuditableTotal)
			}
			if len(narrow.AuditedFiles) >= len(full.AuditedFiles) {
				t.Errorf("the narrowed pass audited %d file(s) and the full pass %d — narrowing "+
					"examined no less than everything", len(narrow.AuditedFiles), len(full.AuditedFiles))
			}

			// And the block an agent actually receives, built by the production
			// function, must declare the partiality.
			// Narrowing decides WHICH files are analysed; it must never change what
			// the analysis of those files concluded. Compared byte-for-byte against
			// the full run restricted to the same files — with the pick biased
			// towards files that actually produced something, so on a project with
			// real findings or surface this is a comparison of real payload and not
			// of two empty slices.
			gotJSON := payloadJSON(t, narrow.Findings, narrow.Surface)
			wantJSON := payloadJSON(t, filterByFile(full.Findings, wanted, func(f findings.Finding) string { return f.File }),
				filterByFile(full.Surface, wanted, func(s findings.SurfaceItem) string { return s.File }))
			if gotJSON != wantJSON {
				t.Errorf("the narrowed pass reached a different verdict on the SAME files than the full "+
					"pass did.\nfull(restricted): %s\nnarrowed:         %s", wantJSON, gotJSON)
			}

			blk := scopeBlockFor(scp, narrow.AuditedFiles, narrow.AuditableTotal)
			if err := blk.Validate(); err != nil {
				t.Errorf("the scope block a real narrowed scan produces is invalid: %v", err)
			}
			if blk.Mode != ScopeModePartial {
				t.Errorf("scope block mode = %q, want %q", blk.Mode, ScopeModePartial)
			}
			if blk.Requested != n || blk.Audited != n || len(blk.Unmatched) != 0 {
				t.Errorf("scope block = requested %d, audited %d, unmatched %v; want %d/%d and none",
					blk.Requested, blk.Audited, blk.Unmatched, n, n)
			}

			t.Logf("SCOPE %-14s narrowed=%d/%d (mode=%s) | full=%d/%d findings full=%d narrowed=%d "+
				"surface full=%d narrowed=%d",
				p.Name, blk.Audited, blk.AuditableTotal, blk.Mode,
				len(full.AuditedFiles), full.AuditableTotal,
				len(full.Findings), len(narrow.Findings), len(full.Surface), len(narrow.Surface))
		})
	}
}
