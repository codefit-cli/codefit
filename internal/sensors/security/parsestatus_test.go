package security

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/codefit-cli/codefit/internal/config"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/golang"
	"github.com/codefit-cli/codefit/internal/providers/typescript"
)

// These tests drive the REAL security sensor, the REAL TypeScript provider and
// the REAL cache (ADR 0089). Every parse status compared below was computed by
// the parser over a file on disk; none is a hand-built value.

// trailingAssignment is an AST-only credential: SEC-001 fires on it through the
// provider's rules, and layer 1's regexes do not match it, so whether it is
// reported says whether the parser read its line. (Its Go name avoids the
// credential vocabulary on purpose: codefit audits its own tree, and the
// self-audit census pins every SEC-001 site it finds.)
const trailingAssignment = `const apiKey = "s3cr3t";`

// brokenProject holds the two measured kinds of broken file, and a clean one as
// the positive control:
//
//   - src/broken.ts: an ERROR node on line 3 (`}}}}`), the secret on line 4.
//     The error is local: the secret is still reported.
//   - src/residual.tsx: residual (c) of ADR 0088 (JSX text starting with `(`
//     right after an attribute-less opening tag). The ERROR node runs into the
//     next statement: the secret on line 2 is LOST. This is the case the status
//     exists for — the file looks clean and is not.
//   - src/clean.ts: the same secret, parsed whole and reported.
func brokenProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeSrc(t, root, "src/broken.ts", "const a = 1;\nconst b = 2;\n}}}}\n"+trailingAssignment+"\n")
	writeSrc(t, root, "src/residual.tsx",
		"export const Hint = () => <span>(optional)</span>;\n"+trailingAssignment+"\n")
	writeSrc(t, root, "src/clean.ts", trailingAssignment+"\n")
	return root
}

// brokenProjectIncomplete is what the sensor must list for brokenProject.
var brokenProjectIncomplete = []findings.IncompleteFile{
	{File: "src/broken.ts", Reason: findings.ParseReasonErrorNode, Line: 3},
	{File: "src/residual.tsx", Reason: findings.ParseReasonErrorNode, Line: 1},
}

func hasFindingAt(fs []findings.Finding, file, id string, line int) bool {
	for _, f := range fs {
		if f.File == file && f.ID == id && f.Line == line {
			return true
		}
	}
	return false
}

// TestParseStatus_BrokenFilesAreListedWithReasonAndLine: the sensor lists every
// file the provider did not parse whole, with its reason and first affected
// line, and leaves the findings exactly as the provider computed them — the
// status is added, nothing is taken away.
func TestParseStatus_BrokenFilesAreListedWithReasonAndLine(t *testing.T) {
	root := brokenProject(t)
	res := run(t, root, typescript.New(), nil)

	if !res.ParseStatusReported {
		t.Error("ParseStatusReported = false with the TypeScript provider, which reports a status for every file")
	}
	if !reflect.DeepEqual(res.ParseIncomplete, brokenProjectIncomplete) {
		t.Errorf("ParseIncomplete = %+v, want %+v", res.ParseIncomplete, brokenProjectIncomplete)
	}

	// The secret outcome, asserted explicitly in every file.
	if !hasFindingAt(res.Findings, "src/broken.ts", "SEC-001", 4) {
		t.Errorf("the secret after the local error in src/broken.ts is not reported: %+v", res.Findings)
	}
	if hasFindingAt(res.Findings, "src/residual.tsx", "SEC-001", 2) {
		t.Errorf("the secret after residual (c) in src/residual.tsx IS reported — the residual no longer "+
			"loses it; re-examine ADR 0088 and this row: %+v", res.Findings)
	}
	if !hasFindingAt(res.Findings, "src/clean.ts", "SEC-001", 1) {
		t.Errorf("positive control: the secret in src/clean.ts is not reported: %+v", res.Findings)
	}
}

// TestParseStatus_IncompleteListIsSortedByPathNotByWalkOrder: the list is
// documented as sorted by path. The layout makes the two orders differ: the walk
// visits the directory `a` (entry name "a") before the file `a.ts`, so it meets
// `a/inner.ts` first, while sorted by path `a.ts` comes first ('.' sorts before
// '/'). The first assertion is the positive control that the layout really
// separates them, so the second cannot pass by coincidence.
func TestParseStatus_IncompleteListIsSortedByPathNotByWalkOrder(t *testing.T) {
	root := t.TempDir()
	broken := "const a = 1;\n}}}}\n"
	writeSrc(t, root, "a.ts", broken)
	writeSrc(t, root, "a/inner.ts", broken)

	var walked []string
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			walked = append(walked, filepath.ToSlash(rel))
		}
		return nil
	}); err != nil {
		t.Fatalf("walking the layout: %v", err)
	}
	if want := []string{"a/inner.ts", "a.ts"}; !reflect.DeepEqual(walked, want) {
		t.Fatalf("positive control: the walk visits %v, want %v — the layout no longer separates walk order "+
			"from path order, so this test would prove nothing", walked, want)
	}

	res := run(t, root, typescript.New(), nil)
	want := []findings.IncompleteFile{
		{File: "a.ts", Reason: findings.ParseReasonErrorNode, Line: 2},
		{File: "a/inner.ts", Reason: findings.ParseReasonErrorNode, Line: 2},
	}
	if !reflect.DeepEqual(res.ParseIncomplete, want) {
		t.Errorf("ParseIncomplete = %+v, want %+v (sorted by path)", res.ParseIncomplete, want)
	}
}

// TestParseStatus_WarmRunFromCacheStillReportsTheStatus: a second run is served
// entirely from the cache — zero re-analysis — and still reports the same files
// as incomplete, byte-identical to the cold run. A cache that dropped the status
// would turn every incomplete file into a clean one on the second scan.
func TestParseStatus_WarmRunFromCacheStillReportsTheStatus(t *testing.T) {
	root := brokenProject(t)
	cfg := cacheConfig(filepath.Join(t.TempDir(), "cache"))

	cold := counting()
	coldRes := run(t, root, cold, cfg)
	if cold.fileCalls == 0 {
		t.Fatal("the cold run never used the combined call — the status was not computed by the provider")
	}
	if !reflect.DeepEqual(coldRes.ParseIncomplete, brokenProjectIncomplete) {
		t.Fatalf("cold ParseIncomplete = %+v, want %+v", coldRes.ParseIncomplete, brokenProjectIncomplete)
	}

	warm := counting()
	warmRes := run(t, root, warm, cfg)
	if warm.calls() != 0 {
		t.Errorf("the warm run re-analysed %d file(s) — it must be served entirely from the cache", warm.calls())
	}
	if !warmRes.ParseStatusReported {
		t.Error("the warm run lost ParseStatusReported")
	}
	if !reflect.DeepEqual(warmRes.ParseIncomplete, brokenProjectIncomplete) {
		t.Errorf("warm ParseIncomplete = %+v, want %+v — the cache dropped the parse status",
			warmRes.ParseIncomplete, brokenProjectIncomplete)
	}
	if got, want := wire(t, warmRes), wire(t, coldRes); got != want {
		t.Errorf("warm differs from cold.\ncold: %s\nwarm: %s", want, got)
	}
}

// TestParseStatus_EntryWithoutStatusIsAMiss (D3): an entry that names its key
// but carries no parse status — what a writer from before the field existed
// leaves — is re-analysed, never served. The entries here are the sensor's own,
// written by the real cache, with only the parse field removed.
func TestParseStatus_EntryWithoutStatusIsAMiss(t *testing.T) {
	root := brokenProject(t)
	dir := filepath.Join(t.TempDir(), "cache")
	cfg := cacheConfig(dir)

	coldRes := run(t, root, counting(), cfg)
	entries := entryFiles(t, dir)
	if len(entries) != 3 {
		t.Fatalf("the cold run wrote %d entries, want 3", len(entries))
	}
	for _, p := range entries {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		if _, ok := m["parse"]; !ok {
			t.Fatalf("entry %s has no parse field to remove — the fixture proves nothing", p)
		}
		delete(m, "parse")
		out, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, out, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	warm := counting()
	warmRes := run(t, root, warm, cfg)
	if warm.fileCalls != 3 {
		t.Errorf("the warm run analysed %d file(s), want all 3 — a status-less entry was served as a hit",
			warm.fileCalls)
	}
	if got, want := wire(t, warmRes), wire(t, coldRes); got != want {
		t.Errorf("re-analysis after a status-less entry differs from the cold run.\ncold: %s\nwarm: %s", want, got)
	}
}

// TestCountingProviderForwardsTheParseStatus guards this package's own test
// wrapper: countingProvider embeds LanguageProvider, which hides every optional
// interface, so it must forward the combined call explicitly. If it stops, the
// sensor takes the two-call path and every cache test above would compare
// results that carry no parse status at all.
func TestCountingProviderForwardsTheParseStatus(t *testing.T) {
	p := counting()
	if _, ok := any(p).(providers.FileAnalyzer); !ok {
		t.Fatal("countingProvider does not implement providers.FileAnalyzer")
	}
	res := run(t, brokenProject(t), p, nil)
	if p.fileCalls != 3 {
		t.Errorf("the combined call ran %d time(s), want 3 — the sensor did not reach it through the wrapper", p.fileCalls)
	}
	if !res.ParseStatusReported || len(res.ParseIncomplete) != 2 {
		t.Errorf("through the wrapper: reported=%v incomplete=%+v, want reported and 2 files",
			res.ParseStatusReported, res.ParseIncomplete)
	}
}

// TestParseStatus_GoFilesAreNotReported: the Go provider reports no parse status,
// and the sensor says so — ParseStatusReported false — rather than presenting an
// empty list as "every file was read whole". Its entries are stored with the
// not-reported status, a present value, so they are hits like any other: a Go
// project's cache is not defeated by D3.
func TestParseStatus_GoFilesAreNotReported(t *testing.T) {
	root := t.TempDir()
	// The key is split in this source so the line here does not match layer 1
	// itself (the self-audit census pins every such site); the file written to
	// disk holds it whole.
	writeSrc(t, root, "main.go", "package main\n\nconst apiKey = \"sk-ant-"+"api03-AbCdEf1234567890\"\n\nfunc main() {}\n")
	dir := filepath.Join(t.TempDir(), "cache")
	cfg := &config.Config{
		Project: config.Project{Language: "go", PathCriticality: config.PathCriticality{Production: []string{"**/*"}}},
		Cache:   config.Cache{Enabled: true, Dir: dir},
	}
	ctx := auditctx.AuditContext{ProjectRoot: root, Language: "go", Config: cfg}

	cold, err := New(golang.New()).Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if cold.ParseStatusReported {
		t.Error("the Go provider reports no parse status, but ParseStatusReported = true")
	}
	if len(cold.ParseIncomplete) != 0 {
		t.Errorf("ParseIncomplete = %+v for a provider that reports nothing", cold.ParseIncomplete)
	}
	if len(cold.Findings) == 0 {
		t.Fatal("positive control: the Go fixture produced no finding")
	}
	entries := entryFiles(t, dir)
	if len(entries) != 1 {
		t.Fatalf("%d cache entries, want 1 — a not-reported status was refused by the cache", len(entries))
	}
	data, err := os.ReadFile(entries[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"parse":{"reported":false,"complete":false}`) {
		t.Errorf("the Go entry does not store the not-reported status: %s", data)
	}

	warm, err := New(golang.New()).Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := wire(t, warm), wire(t, cold); got != want {
		t.Errorf("warm differs from cold for Go.\ncold: %s\nwarm: %s", want, got)
	}
}

// TestParseStatus_BOMLessUTF16FileIsReportedIncomplete measures what a BOM-less
// UTF-16 file gets. sourcetext.Decode refuses to guess its encoding, so the
// parser reads NUL-interleaved bytes (the declared residual of ADR 0044). On the
// pinned parser that tree holds an ERROR node on line 1, so the file is now LISTED
// as not parsed whole — where before it yielded nothing, silently. The residual
// is not closed: the file is still not analysed, and its secret is still lost;
// it is no longer silent in the sensor result.
func TestParseStatus_BOMLessUTF16FileIsReportedIncomplete(t *testing.T) {
	root := t.TempDir()
	units := utf16.Encode([]rune(trailingAssignment + "\n"))
	raw := make([]byte, 0, 2*len(units))
	for _, u := range units {
		raw = append(raw, byte(u), byte(u>>8)) // little-endian, no byte-order mark
	}
	writeSrc(t, root, "src/utf16.ts", string(raw))

	res := run(t, root, typescript.New(), nil)
	want := []findings.IncompleteFile{{File: "src/utf16.ts", Reason: findings.ParseReasonErrorNode, Line: 1}}
	if !reflect.DeepEqual(res.ParseIncomplete, want) {
		t.Errorf("ParseIncomplete = %+v, want %+v", res.ParseIncomplete, want)
	}
	if len(res.Findings) != 0 {
		t.Errorf("findings = %+v — the declared residual is closed; re-examine ADR 0044 and this row", res.Findings)
	}
}

// TestPlantedEntryIsAHitWhenTheCacheIsOn is the positive control of
// TestCache_DisabledReadsNothing: the entry plantPoisonedEntry writes IS served
// when the cache is on. Without it, a planted entry that would be a miss anyway
// (a missing parse status, a key the sensor no longer computes) would make
// "disabled reads nothing" pass by vacuum.
func TestPlantedEntryIsAHitWhenTheCacheIsOn(t *testing.T) {
	root := tsProject(t)
	dir := filepath.Join(t.TempDir(), "cache")
	plantPoisonedEntry(t, root, dir, "src/clean.ts")

	res := run(t, root, counting(), cacheConfig(dir))
	if !hasFindingID(res.Findings, "POISON-001") {
		t.Error("the planted entry was not served with the cache on — TestCache_DisabledReadsNothing proves nothing")
	}
}

// --- memory-budget stop, in a child process ----------------------------------

// The parser reads GOT_PARSE_MEMORY_BUDGET_MB once per process (sync.Once), so a
// budget can only be set for a whole process. These tests re-run this test
// binary as a child with the variable set, the child runs the real sensor and
// writes its result to a file, and the parent asserts on it.
const (
	childEnv    = "CODEFIT_TEST_SENSOR_CHILD"
	childRoot   = "CODEFIT_TEST_SENSOR_CHILD_ROOT"
	childCache  = "CODEFIT_TEST_SENSOR_CHILD_CACHE"
	childOutput = "CODEFIT_TEST_SENSOR_CHILD_OUTPUT"
	budgetEnv   = "GOT_PARSE_MEMORY_BUDGET_MB"
)

// stopBudgetMB is the budget the stop tests run under. Measured on gotreesitter
// v0.54.0, linux/amd64, through this sensor (the child below), with and without
// -race — identical both ways:
//
//	budget MB   real_vscode_strings.ts (92 KB)   a 194-byte file
//	8, 7        accepted                         accepted
//	6, 5        stopped: memory_budget           accepted
//	4, 3, 2     the scan fails: the rule patterns themselves no longer
//	            compile (the raw parser, without the rules, also stops the
//	            194-byte file at 4 and 3)
//
// 5 sits one MB above the first failing budget and two below the first that
// stops nothing. The soft budget counts what the parse allocates in its own
// arenas, not the process heap (gotreesitter's issue #454 determinism contract),
// so the same bytes stop at the same place on every run: -count=10 passed with
// and without -race.
const stopBudgetMB = "5"

// ruleCompileFailsBudgetMB is a budget so low that the embedded rule patterns
// cannot be parsed: the scan fails loudly instead of reporting files. Every
// budget from 4 down was measured failing; 2 is the lowest, furthest from the
// window.
const ruleCompileFailsBudgetMB = "2"

type childOutcome struct {
	Result findings.SensorResult `json:"result"`
	Error  string                `json:"error"`
}

// TestHelperProcessSecuritySensor is not a test: it is the child process the
// budget tests run. Without its environment variable it does nothing.
func TestHelperProcessSecuritySensor(t *testing.T) {
	if os.Getenv(childEnv) != "1" {
		t.Skip("helper process for the parse-budget tests; runs only as their child")
	}
	var cfg *config.Config
	if dir := os.Getenv(childCache); dir != "" {
		cfg = cacheConfig(dir)
	}
	res, err := New(typescript.New()).Run(auditctx.AuditContext{
		ProjectRoot: os.Getenv(childRoot), Language: "ts", Config: cfg,
	})
	out := childOutcome{Result: res}
	if err != nil {
		out.Error = err.Error()
	}
	data, mErr := json.Marshal(out)
	if mErr != nil {
		t.Fatal(mErr)
	}
	if wErr := os.WriteFile(os.Getenv(childOutput), data, 0o600); wErr != nil {
		t.Fatal(wErr)
	}
}

// runChild runs the sensor over root in a child process whose environment sets
// GOT_PARSE_MEMORY_BUDGET_MB to budget, optionally with the cache at cacheDir.
func runChild(t *testing.T, budget, root, cacheDir string) childOutcome {
	t.Helper()
	output := filepath.Join(t.TempDir(), "child.json")
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcessSecuritySensor$", "-test.count=1")
	cmd.Env = append(os.Environ(),
		childEnv+"=1", childRoot+"="+root, childCache+"="+cacheDir, childOutput+"="+output,
		budgetEnv+"="+budget)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child process failed: %v\n%s", err, out)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("the child wrote no result: %v", err)
	}
	var oc childOutcome
	if err := json.Unmarshal(data, &oc); err != nil {
		t.Fatalf("decoding the child result: %v", err)
	}
	return oc
}

// budgetProject is a project with one large real file the stop budget stops,
// and one small file with an AST-only secret that the same budget does not stop.
func budgetProject(t *testing.T) string {
	t.Helper()
	big, err := os.ReadFile(filepath.Join("..", "..", "providers", "typescript", "testdata", "real_vscode_strings.ts"))
	if err != nil {
		t.Fatalf("reading the large fixture: %v", err)
	}
	root := t.TempDir()
	writeSrc(t, root, "src/strings.ts", string(big))
	writeSrc(t, root, "src/small.ts", trailingAssignment+"\n")
	return root
}

// TestParseStatus_MemoryBudgetStopIsReported (S11, S12): a parse stopped by the
// memory budget is reported as stopped:memory_budget — the stop outranks the
// one-node ERROR root it leaves — and, in the same run, a small file the budget
// does not stop is complete and keeps its finding. The control run without the
// budget proves the large file is complete when nothing stops it.
func TestParseStatus_MemoryBudgetStopIsReported(t *testing.T) {
	root := budgetProject(t)

	stopped := runChild(t, stopBudgetMB, root, "")
	if stopped.Error != "" {
		t.Fatalf("the scan under a %s MB budget failed: %s", stopBudgetMB, stopped.Error)
	}
	want := []findings.IncompleteFile{{File: "src/strings.ts", Reason: "stopped:memory_budget", Line: 1}}
	if !reflect.DeepEqual(stopped.Result.ParseIncomplete, want) {
		t.Errorf("under a %s MB budget, ParseIncomplete = %+v, want %+v",
			stopBudgetMB, stopped.Result.ParseIncomplete, want)
	}
	if !stopped.Result.ParseStatusReported {
		t.Error("ParseStatusReported = false under the budget")
	}
	if !hasFindingAt(stopped.Result.Findings, "src/small.ts", "SEC-001", 1) {
		t.Errorf("positive control: the small file's secret is lost under the budget: %+v", stopped.Result.Findings)
	}

	control := run(t, root, typescript.New(), nil)
	if len(control.ParseIncomplete) != 0 {
		t.Errorf("control without a budget: ParseIncomplete = %+v, want none", control.ParseIncomplete)
	}
}

// TestParseStatus_BudgetTooLowForTheRulesFailsTheScan: the detection's other
// edge, declared rather than reported. Under a budget too small to parse the
// embedded rule patterns, the scan returns an ERROR — loudly — instead of
// reporting every file as stopped and the project as clean.
func TestParseStatus_BudgetTooLowForTheRulesFailsTheScan(t *testing.T) {
	oc := runChild(t, ruleCompileFailsBudgetMB, budgetProject(t), "")
	if !strings.Contains(oc.Error, "compiling typescript security rules") {
		t.Errorf("under a %s MB budget the scan error is %q, want the rule-compilation failure",
			ruleCompileFailsBudgetMB, oc.Error)
	}
}

// --- parser settings in the cache identity (D2) -------------------------------

// settingsProbe is a variable under the parser's GOT_ prefix that the parser
// does not read: changing it moves the fingerprint without changing any parse,
// so it can be set in this process without disturbing other tests.
const settingsProbe = "GOT_CODEFIT_FINGERPRINT_PROBE"

// TestCache_ParserSettingsAreKeyedInProcess (D2): the sensor keys the cache on
// its provider's settings fingerprint. The same files, the same binary, another
// value of a parser-prefixed variable: every file is a miss; the same value
// again: every file is a hit.
func TestCache_ParserSettingsAreKeyedInProcess(t *testing.T) {
	root := tsProject(t)
	cfg := cacheConfig(filepath.Join(t.TempDir(), "cache"))

	t.Setenv(settingsProbe, "1")
	run(t, root, counting(), cfg)

	t.Setenv(settingsProbe, "2")
	other := counting()
	run(t, root, other, cfg)
	if other.fileCalls != 3 {
		t.Errorf("under another parser setting %d of 3 files were re-analysed — entries computed under "+
			"the first setting were served under the second", other.fileCalls)
	}

	same := counting()
	run(t, root, same, cfg)
	if same.calls() != 0 {
		t.Errorf("under the same setting %d file(s) were re-analysed — the fingerprint is not stable", same.calls())
	}
}

// TestParseStatus_ParserSettingsArePartOfTheCacheIdentity (D2, end to end): a
// child run under a memory budget writes the cache with the large file stopped;
// a run of the SAME binary without the budget, over the same cache, must not be
// served that entry. It re-analyses, reports every file complete, and equals an
// uncached run. Without the settings in the key, the second run would report the
// first run's truncated analysis as its own.
func TestParseStatus_ParserSettingsArePartOfTheCacheIdentity(t *testing.T) {
	root := budgetProject(t)
	dir := filepath.Join(t.TempDir(), "cache")

	child := runChild(t, stopBudgetMB, root, dir)
	if child.Error != "" {
		t.Fatalf("the child scan failed: %s", child.Error)
	}
	if len(child.Result.ParseIncomplete) != 1 {
		t.Fatalf("the child did not stop the large file (incomplete = %+v) — nothing to keep apart",
			child.Result.ParseIncomplete)
	}
	if n := len(entryFiles(t, dir)); n != 2 {
		t.Fatalf("the child wrote %d cache entries, want 2", n)
	}

	parent := counting()
	res := run(t, root, parent, cacheConfig(dir))
	if parent.fileCalls != 2 {
		t.Errorf("without the budget %d of 2 files were re-analysed — the run was served entries "+
			"computed under the budget", parent.fileCalls)
	}
	if !res.ParseStatusReported || len(res.ParseIncomplete) != 0 {
		t.Errorf("without the budget: reported=%v incomplete=%+v, want every file complete",
			res.ParseStatusReported, res.ParseIncomplete)
	}
	control := run(t, root, typescript.New(), &config.Config{Project: cacheConfig(dir).Project})
	if got, want := wire(t, res), wire(t, control); got != want {
		t.Errorf("the run after a budgeted run differs from an uncached run.\nuncached: %s\ngot:      %s", want, got)
	}
}

// TestCountingProviderForwardsTheSettingsFingerprint guards the test wrapper the
// same way as for the combined call: the embedded interface hides
// SettingsFingerprinter, so the wrapper must forward it or every cache test
// would run with no settings in its keys.
func TestCountingProviderForwardsTheSettingsFingerprint(t *testing.T) {
	t.Setenv(settingsProbe, "1")
	sf, ok := any(counting()).(providers.SettingsFingerprinter)
	if !ok {
		t.Fatal("countingProvider does not implement providers.SettingsFingerprinter")
	}
	want := typescript.New().SettingsFingerprint()
	if got := sf.SettingsFingerprint(); want == "" || got != want {
		t.Errorf("wrapper fingerprint %q, provider fingerprint %q — want equal and non-empty", got, want)
	}
}
