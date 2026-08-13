package security

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	"github.com/codefit-cli/codefit/internal/core/cache"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

// These tests drive the REAL sensor over REAL files with the REAL TypeScript
// provider. Nothing here hand-assembles a SensorResult or a cache entry: every
// finding and surface item compared below was computed by the production path,
// because a fixture holding values that path cannot produce locks in a reality
// that does not exist.

// countingProvider wraps a real provider and counts the files it analyses. It
// performs no analysis of its own; `extra` exists only for the one test that
// needs a DIFFERENT analyzer — a newer codefit shipping a rule the old one did
// not have, which is exactly what the analyzer identity in the key defends
// against.
type countingProvider struct {
	providers.LanguageProvider
	securityCalls int
	surfaceCalls  int
	extra         string // when set, an extra finding ID emitted for every file
}

func (p *countingProvider) AnalyzeSecurity(src providers.SourceFile) ([]findings.Finding, error) {
	p.securityCalls++
	fs, err := p.LanguageProvider.AnalyzeSecurity(src)
	if err != nil {
		return nil, err
	}
	if p.extra != "" {
		fs = append(fs, findings.Finding{
			ID: p.extra, Dimension: findings.DimensionSecurity,
			Severity: findings.SeverityMedium, File: src.Path, Line: 1,
			Title: "a rule the newer analyzer ships", Confidence: 1.0,
		})
	}
	return fs, nil
}

func (p *countingProvider) AnalyzeSurface(src providers.SourceFile) ([]findings.SurfaceItem, error) {
	p.surfaceCalls++
	return p.LanguageProvider.AnalyzeSurface(src)
}

func (p *countingProvider) calls() int { return p.securityCalls + p.surfaceCalls }

func counting() *countingProvider {
	return &countingProvider{LanguageProvider: typescript.New()}
}

func writeSrc(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

const nextRoute = `export async function GET(req: Request, { params }: { params: { id: string } }) {
  return Response.json(await prisma.user.findUnique({ where: { id: params.id } }));
}
`

// tsProject is a project that produces BOTH real findings and real surface: a
// leaked credential (SEC-001, layer 1 + layer 2) and a Next App Router route
// reading params.id (IDOR surface, layer 2). A cache test over a project with
// only one of the two would pass while silently dropping the other.
func tsProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeSrc(t, root, "src/secrets.ts", "const apiKey = \"sk-ant-api03-AbCdEf1234567890\";\n")
	writeSrc(t, root, "app/users/[id]/route.ts", nextRoute)
	writeSrc(t, root, "src/clean.ts", "export function add(a: number, b: number) { return a + b; }\n")
	return root
}

func cacheConfig(cacheDir string) *config.Config {
	return &config.Config{
		Project: config.Project{
			Language:        "ts",
			PathCriticality: config.PathCriticality{Production: []string{"**/*"}},
		},
		Cache: config.Cache{Enabled: true, Dir: cacheDir},
	}
}

func run(t *testing.T, root string, p providers.LanguageProvider, cfg *config.Config) findings.SensorResult {
	t.Helper()
	res, err := New(p).Run(auditctx.AuditContext{ProjectRoot: root, Language: "ts", Config: cfg})
	if err != nil {
		t.Fatalf("sensor Run: %v", err)
	}
	return res
}

// wire is the result JSON a consumer sees, with the wall-clock duration zeroed:
// how long the run took is a measurement OF the run, not part of what the run
// concluded, and a cache that works necessarily changes it.
func wire(t *testing.T, res findings.SensorResult) string {
	t.Helper()
	res.DurationMs = 0
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Contract item 1 / R1 — THE definition of done. A cold scan and a warm scan of
// the same project are byte-identical, over a project with real findings AND
// real surface. Not equivalent: identical. A cache that can change the output is
// not a cache, it is a blind spot.
func TestCache_ColdAndWarmAreByteIdentical(t *testing.T) {
	root := tsProject(t)
	cfg := cacheConfig(filepath.Join(t.TempDir(), "cache"))

	cold := counting()
	coldRes := run(t, root, cold, cfg)

	// Guard against passing by vacuum: this project must really produce both.
	if len(coldRes.Findings) == 0 {
		t.Fatal("the cold scan produced no findings — the fixture cannot prove anything")
	}
	if len(coldRes.Surface) == 0 {
		t.Fatal("the cold scan produced no surface — a findings-only cache would pass this test")
	}
	if cold.calls() == 0 {
		t.Fatal("the cold scan analysed nothing")
	}

	warm := counting()
	warmRes := run(t, root, warm, cfg)

	if warm.securityCalls != 0 || warm.surfaceCalls != 0 {
		t.Errorf("the warm scan re-analysed %d file(s) — nothing was served from the cache, "+
			"so this test would compare two cold runs", warm.calls())
	}
	if got, want := wire(t, warmRes), wire(t, coldRes); got != want {
		t.Errorf("warm scan differs from cold scan.\ncold: %s\nwarm: %s", want, got)
	}
}

// Contract item 2 — changed bytes are a miss and the file is recomputed.
func TestCache_ChangedBytesAreAMissAndRecomputed(t *testing.T) {
	root := tsProject(t)
	cfg := cacheConfig(filepath.Join(t.TempDir(), "cache"))

	run(t, root, counting(), cfg)

	// The clean file gains a credential.
	writeSrc(t, root, "src/clean.ts", "const token = \"ghp_ABCDEFGHIJKLMNOPQRST\";\n")

	warm := counting()
	res := run(t, root, warm, cfg)

	if warm.securityCalls != 1 {
		t.Errorf("AnalyzeSecurity ran on %d file(s), want exactly 1 — only the changed file", warm.securityCalls)
	}
	if !hasFinding(res.Findings, "src/clean.ts", "SEC-001") {
		t.Errorf("the changed file's new credential was not reported: %+v", res.Findings)
	}
}

// Contract item 3 / R2 — a different analyzer is a miss even when every file is
// unchanged. A codefit upgrade ships new rules; a cache keyed on the file alone
// would answer with findings computed under the OLD rules and report "clean"
// under rules it never ran.
func TestCache_DifferentAnalyzerIdentityIsAMiss(t *testing.T) {
	root := tsProject(t)
	dir := filepath.Join(t.TempDir(), "cache")
	cfg := cacheConfig(dir)

	pinAnalyzer(t, "analyzer-v1")
	old := counting()
	oldRes := run(t, root, old, cfg)
	if hasFindingID(oldRes.Findings, "SEC-999") {
		t.Fatal("the old analyzer already emits SEC-999 — the fixture proves nothing")
	}

	// A newer codefit: same files, same bytes, a binary that ships one more rule.
	pinAnalyzer(t, "analyzer-v2")
	upgraded := counting()
	upgraded.extra = "SEC-999"
	newRes := run(t, root, upgraded, cfg)

	if upgraded.securityCalls == 0 {
		t.Error("the upgraded analyzer re-used the previous analyzer's entries — every file was a hit")
	}
	if !hasFindingID(newRes.Findings, "SEC-999") {
		t.Error("the new analyzer's rule never fired: the scan reported a verdict the running " +
			"analyzer never computed (R2)")
	}
}

// Contract item 4 / R2 — when the analyzer identity cannot be resolved the cache
// disables itself, and the scan still completes FULLY. Unknown key inputs mean do
// not reuse; they never mean do not audit.
func TestCache_IdentityUnavailableDisablesTheCacheAndTheScanCompletes(t *testing.T) {
	root := tsProject(t)
	dir := filepath.Join(t.TempDir(), "cache")
	cfg := cacheConfig(dir)

	// A reference run with caching genuinely off, to compare against.
	reference := run(t, root, counting(), &config.Config{Project: cfg.Project})

	failOpen(t)
	first := counting()
	firstRes := run(t, root, first, cfg)
	second := counting()
	secondRes := run(t, root, second, cfg)

	ref := wire(t, reference)
	if got := wire(t, firstRes); got != ref {
		t.Errorf("a scan with an unresolvable analyzer identity is not a full scan.\nwant: %s\ngot:  %s", ref, got)
	}
	if second.securityCalls != first.securityCalls || first.securityCalls == 0 {
		t.Errorf("every file must be analysed on every run when the identity is unknown: "+
			"first=%d second=%d", first.securityCalls, second.securityCalls)
	}
	if got := wire(t, secondRes); got != ref {
		t.Errorf("the second scan is not a full scan either.\nwant: %s\ngot:  %s", ref, got)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("a cache directory was created (%q) although the analyzer identity is unknown", dir)
	}
}

// Contract item 5 / R3 — path_criticality is applied AFTER the per-file analysis,
// so a cached entry holds the PRE-criticality output and an edit to .codefit.yaml
// re-weights severities on a WARM cache without invalidating a single entry.
func TestCache_PathCriticalityAppliesOnAWarmCache(t *testing.T) {
	root := t.TempDir()
	writeSrc(t, root, "src/secrets.ts", "const apiKey = \"sk-ant-api03-AbCdEf1234567890\";\n")
	dir := filepath.Join(t.TempDir(), "cache")

	production := cacheConfig(dir)
	coldRes := run(t, root, counting(), production)
	cold := findingIn(t, coldRes.Findings, "src/secrets.ts", "SEC-001")
	if cold.Severity != findings.SeverityCritical {
		t.Fatalf("cold severity = %q, want critical — the fixture cannot show a re-weighting", cold.Severity)
	}
	if !cold.RequiresConsent {
		t.Fatal("a critical security finding must require consent; the fixture is wrong")
	}

	// The developer edits .codefit.yaml: that path is now classified as test.
	asTest := cacheConfig(dir)
	asTest.Project.PathCriticality = config.PathCriticality{Test: []string{"src/**"}}
	warm := counting()
	warmRes := run(t, root, warm, asTest)

	if warm.calls() != 0 {
		t.Errorf("a path_criticality edit invalidated %d cache entr(ies) — it must invalidate none, "+
			"the entry holds the analysis, not the weighting", warm.calls())
	}
	got := findingIn(t, warmRes.Findings, "src/secrets.ts", "SEC-001")
	// EXACT, and deliberately different from the cold severity asserted above.
	// The delta IS the proof: a severity-agnostic assertion would pass just as
	// happily on a cache that replayed the old weighting, which is the failure
	// this test exists to catch. info is the RF-10 default mode
	// (sensors.security.test_severity unset in cacheConfig).
	if got.Severity != findings.SeverityInfo {
		t.Errorf("warm severity = %q, want info (a test path is forced to info under the default "+
			"test_severity mode) — the cache served a severity computed under the OLD config", got.Severity)
	}
	if got.RequiresConsent {
		t.Error("a re-weighted finding is no longer critical and must no longer require consent; " +
			"the cache served the old flag")
	}
}

// Contract item 6 / R4 — a corrupt entry is a MISS and the file is analysed. The
// cache may never be the reason an audit does not happen.
func TestCache_CorruptEntryIsAMissAndTheScanCompletes(t *testing.T) {
	root := tsProject(t)
	dir := filepath.Join(t.TempDir(), "cache")
	cfg := cacheConfig(dir)

	coldRes := run(t, root, counting(), cfg)
	corruptEveryEntry(t, dir)

	warm := counting()
	warmRes := run(t, root, warm, cfg)

	if warm.securityCalls == 0 {
		t.Error("a corrupt cache was read as a hit — the files were never analysed")
	}
	if got, want := wire(t, warmRes), wire(t, coldRes); got != want {
		t.Errorf("a corrupt cache changed the result.\nwant: %s\ngot:  %s", want, got)
	}
}

// Contract item 6 / R4 — a cache that cannot be WRITTEN is a note, not a failed
// scan.
func TestCache_UnwritableCacheStillCompletesTheScan(t *testing.T) {
	root := tsProject(t)
	// A regular FILE where the cache directory should be: MkdirAll cannot succeed.
	blocked := filepath.Join(t.TempDir(), "cache")
	if err := os.WriteFile(blocked, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := cacheConfig(blocked)

	res := run(t, root, counting(), cfg)
	reference := run(t, root, counting(), &config.Config{Project: cfg.Project})

	if got, want := wire(t, res), wire(t, reference); got != want {
		t.Errorf("an unwritable cache changed the result.\nwant: %s\ngot:  %s", want, got)
	}
}

// Contract item 7 / R4 — the cache is OPT-IN. A project with no cache: section
// behaves exactly as before: no directory is created and nothing is read.
func TestCache_DisabledByDefaultCreatesNothing(t *testing.T) {
	root := tsProject(t)
	cfg := &config.Config{Project: config.Project{
		Language:        "ts",
		PathCriticality: config.PathCriticality{Production: []string{"**/*"}},
	}}
	if cfg.Cache.Enabled {
		t.Fatal("config.Cache.Enabled defaults to true — the cache is no longer opt-in")
	}

	first := counting()
	run(t, root, first, cfg)
	second := counting()
	run(t, root, second, cfg)

	if second.securityCalls != first.securityCalls || first.securityCalls == 0 {
		t.Errorf("a disabled cache changed how much was analysed: first=%d second=%d",
			first.securityCalls, second.securityCalls)
	}
	if _, err := os.Stat(filepath.Join(root, ".codefit")); !os.IsNotExist(err) {
		t.Error("a scan with the cache disabled created .codefit/ in the project")
	}
}

// Contract item 7 / R4 — disabled means nothing is READ either, not merely that
// nothing is written. Proven positively: an entry that WOULD be a hit is planted
// where the sensor would look, and must not reach the result.
func TestCache_DisabledReadsNothing(t *testing.T) {
	root := tsProject(t)
	cfg := &config.Config{Project: config.Project{
		Language:        "ts",
		PathCriticality: config.PathCriticality{Production: []string{"**/*"}},
	}}
	plantPoisonedEntry(t, root, filepath.Join(root, ".codefit", "cache"), "src/clean.ts")

	res := run(t, root, counting(), cfg)

	if hasFindingID(res.Findings, "POISON-001") {
		t.Error("a disabled cache was still read: a planted entry reached the result")
	}
}

// R1, the case the spec's key formula does not name — two files with IDENTICAL
// bytes. codefit's own fixtures do this. Sharing one entry between them would
// report the first file's path (and its fingerprint) for the second, so the
// entry key names the file's path as well as its bytes.
func TestCache_IdenticalBytesAtDifferentPathsDoNotShareAnEntry(t *testing.T) {
	root := t.TempDir()
	const leaking = "const apiKey = \"sk-ant-api03-AbCdEf1234567890\";\n"
	writeSrc(t, root, "src/a.ts", leaking)
	writeSrc(t, root, "src/b.ts", leaking)
	cfg := cacheConfig(filepath.Join(t.TempDir(), "cache"))

	res := run(t, root, counting(), cfg)

	if !hasFinding(res.Findings, "src/a.ts", "SEC-001") || !hasFinding(res.Findings, "src/b.ts", "SEC-001") {
		t.Errorf("both files leak a credential but the findings are %+v — two files with identical "+
			"bytes shared a cache entry, so one is reported under the other's path", res.Findings)
	}
	if a, b := findingIn(t, res.Findings, "src/a.ts", "SEC-001"), findingIn(t, res.Findings, "src/b.ts", "SEC-001"); a.Fingerprint == b.Fingerprint {
		t.Error("the two findings share a baseline fingerprint — accepting one would silence the other")
	}
}

// Contract item 8 — a file that produced NOTHING caches that empty result and is
// not analysed again. Otherwise the clean files, which are the majority, never
// benefit and the cache quietly does nothing on a healthy repository.
func TestCache_EmptyResultIsCachedAndNotReanalysed(t *testing.T) {
	root := t.TempDir()
	writeSrc(t, root, "src/clean.ts", "export function add(a: number, b: number) { return a + b; }\n")
	cfg := cacheConfig(filepath.Join(t.TempDir(), "cache"))

	cold := counting()
	coldRes := run(t, root, cold, cfg)
	if len(coldRes.Findings) != 0 || len(coldRes.Surface) != 0 {
		t.Fatalf("the fixture is not clean: %+v / %+v", coldRes.Findings, coldRes.Surface)
	}
	if cold.securityCalls != 1 {
		t.Fatalf("cold AnalyzeSecurity calls = %d, want 1", cold.securityCalls)
	}

	warm := counting()
	warmRes := run(t, root, warm, cfg)

	if warm.calls() != 0 {
		t.Errorf("a clean file was re-analysed on a warm cache (%d call(s)) — an empty result "+
			"was not stored, so the cache does nothing on a healthy repository", warm.calls())
	}
	if got, want := wire(t, warmRes), wire(t, coldRes); got != want {
		t.Errorf("warm differs from cold on a clean project.\nwant: %s\ngot:  %s", want, got)
	}
}

// R4 — an empty cache.dir defaults to .codefit/cache inside the project, which is
// already gitignored and already skipped by the walk.
func TestCache_EmptyDirDefaultsToDotCodefitCache(t *testing.T) {
	root := tsProject(t)
	cfg := cacheConfig("") // dir deliberately unset

	cold := run(t, root, counting(), cfg)

	dir := filepath.Join(root, ".codefit", "cache")
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("stat %q: %v — an unset cache.dir must default to .codefit/cache", dir, err)
	}
	if n := len(entryFiles(t, dir)); n != len(cold.AuditedFiles) {
		t.Errorf("%q holds %d entries, want one per analysed file (%d)", dir, n, len(cold.AuditedFiles))
	}
	// And the cache directory must not become part of what the walk audits.
	warm := counting()
	res := run(t, root, warm, cfg)
	if warm.calls() != 0 {
		t.Errorf("the default cache directory was not re-used: %d call(s)", warm.calls())
	}
	for _, f := range res.AuditedFiles {
		if strings.HasPrefix(f, ".codefit/") {
			t.Errorf("the walk audited its own cache directory: %q", f)
		}
	}
}

// P0-3 (ADR 0053, "A hole this fix does NOT close: the empty read") reproduced
// and disproved the cache half of that declared limit: os.ReadFile CAN observe
// a file as empty (a truncate-then-write race), but the cache key is
// sha256(analyzer ‖ path ‖ CONTENT), so an entry cached from empty content and
// an entry for real content at the SAME path are two different keys. A pass
// over the real content is therefore always a MISS, never a served hit from
// the empty read. This locks that structural property so it cannot regress
// silently, and proves it against the REAL sensor and the REAL cache — not a
// hand-built Entry — with an uncached control run in the assertion so the test
// proves EQUIVALENCE to no cache, not merely "found something".
func TestCache_EmptyReadNeverPoisonsLaterRealContentAtSamePath(t *testing.T) {
	root := t.TempDir()
	const rel = "src/race.ts"
	const leaking = "const apiKey = \"sk-ant-api03-AbCdEf1234567890\";\n"
	cfg := cacheConfig(filepath.Join(t.TempDir(), "cache"))

	// pass1: the file is observed EMPTY — the truncate-then-write race ADR 0053
	// describes. That "nothing" gets cached under the key for empty content at
	// this path.
	writeSrc(t, root, rel, "")
	pass1 := run(t, root, counting(), cfg)
	if hasFinding(pass1.Findings, rel, "SEC-001") {
		t.Fatalf("the fixture is wrong: pass1 must observe the file as empty, got %+v", pass1.Findings)
	}

	// pass2: the SAME path now holds real, leaking content — the write finished.
	writeSrc(t, root, rel, leaking)
	pass2 := counting()
	pass2Res := run(t, root, pass2, cfg)

	// control: the identical real content, scanned with NO cache at all. If the
	// cache is innocent, pass2 must match this exactly — not just "have a
	// finding", but be byte-identical to what an uncached scan produces.
	controlRoot := t.TempDir()
	writeSrc(t, controlRoot, rel, leaking)
	controlRes := run(t, controlRoot, counting(), &config.Config{Project: cfg.Project})

	if pass2.securityCalls == 0 {
		t.Error("pass2 was served from the cache (0 AnalyzeSecurity calls) — the empty-content entry " +
			"was reused for real content at the same path, which is exactly the poisoning ADR 0053 warned about")
	}
	if !hasFinding(pass2Res.Findings, rel, "SEC-001") {
		t.Errorf("pass2 never reported the real leaking content: %+v", pass2Res.Findings)
	}
	if got, want := wire(t, pass2Res), wire(t, controlRes); got != want {
		t.Errorf("pass2 (cached) differs from the uncached control — the cache changed the verdict.\n"+
			"cached:      %s\nuncached: %s", got, want)
	}
}

// --- helpers -------------------------------------------------------------

func hasFinding(fs []findings.Finding, file, id string) bool {
	for i := range fs {
		if fs[i].File == file && fs[i].ID == id {
			return true
		}
	}
	return false
}

func hasFindingID(fs []findings.Finding, id string) bool {
	for i := range fs {
		if fs[i].ID == id {
			return true
		}
	}
	return false
}

func findingIn(t *testing.T, fs []findings.Finding, file, id string) findings.Finding {
	t.Helper()
	for i := range fs {
		if fs[i].File == file && fs[i].ID == id {
			return fs[i]
		}
	}
	t.Fatalf("no %s finding in %s; findings = %+v", id, file, fs)
	return findings.Finding{}
}

// pinAnalyzer makes the sensor open caches bound to a chosen analyzer identity,
// so "a different codefit build" is expressible without shipping two binaries.
func pinAnalyzer(t *testing.T, id string) {
	t.Helper()
	prev := openCache
	openCache = func(dir string) (*cache.Cache, error) {
		return &cache.Cache{Dir: dir, Analyzer: id}, nil
	}
	t.Cleanup(func() { openCache = prev })
}

// failOpen makes the analyzer identity unresolvable for the duration of a test.
func failOpen(t *testing.T) {
	t.Helper()
	prev := openCache
	openCache = func(string) (*cache.Cache, error) {
		return nil, os.ErrNotExist
	}
	t.Cleanup(func() { openCache = prev })
}

// entryFiles are the cache's entry files, wherever under dir the layout puts
// them: they live in a per-generation subdirectory, so a listing of dir alone
// would find directories and no entries at all.
func entryFiles(t *testing.T, dir string) []string {
	t.Helper()
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

func corruptEveryEntry(t *testing.T, dir string) {
	t.Helper()
	entries := entryFiles(t, dir)
	if len(entries) == 0 {
		t.Fatal("the cache dir holds no entries — nothing to corrupt, the test proves nothing")
	}
	for _, p := range entries {
		if err := os.WriteFile(p, []byte("}{ not json"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// plantPoisonedEntry writes a real cache entry, at the real key the sensor would
// compute, carrying a finding no analysis produces.
func plantPoisonedEntry(t *testing.T, root, dir, rel string) {
	t.Helper()
	c, err := cache.Open(dir)
	if err != nil {
		t.Fatalf("opening the cache: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	err = c.Set(c.Key(rel, raw), cache.Entry{Findings: []findings.Finding{{
		ID: "POISON-001", Dimension: findings.DimensionSecurity,
		Severity: findings.SeverityCritical, File: rel, Line: 1, Confidence: 1.0,
	}}})
	if err != nil {
		t.Fatalf("planting a cache entry: %v", err)
	}
}
