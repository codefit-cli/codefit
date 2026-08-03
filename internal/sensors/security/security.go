// Package security is the universal security sensor. It orchestrates the
// filtering pyramid — a language-agnostic regex layer for obvious secrets and
// the active provider's AST analysis (deterministic findings plus mapped
// surface) — and adjusts each finding's severity by the file's path criticality
// (RF-11). codefit runs no LLM layer: the surface is returned to the agent,
// which reasons over it with its own model.
package security

import (
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/codefit-cli/codefit/internal/config"
	"github.com/codefit-cli/codefit/internal/core/cache"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/scoring"
	"github.com/codefit-cli/codefit/internal/core/sourcetext"
	"github.com/codefit-cli/codefit/internal/core/surface"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/sensors"
)

// Sensor runs the security dimension against a project, driven by a language
// provider for the AST layer.
type Sensor struct {
	provider providers.LanguageProvider
}

// New builds a security sensor backed by a language provider.
func New(p providers.LanguageProvider) *Sensor { return &Sensor{provider: p} }

func (*Sensor) Name() string                  { return "security" }
func (*Sensor) Dimension() findings.Dimension { return findings.DimensionSecurity }

// compile-time check that the security sensor satisfies the Sensor contract.
var _ sensors.Sensor = (*Sensor)(nil)

// OwnedCategories are the baseline Item categories the security sensor produces:
// its finding dimension ("security") plus its surface categories (idor/authz/
// overfetch/nplus1). They scope the unified baseline (ADR 0019). CategoryNPlus1
// is included even though its agent-confirmed finding is stamped DimensionDB
// (dimensionFor) — this sensor is what ENUMERATES the surface item, so it must
// own the category for baseline tracking/pruning, regardless of which dimension
// a later confirmation lands in.
func (*Sensor) OwnedCategories() []string {
	return []string{
		string(findings.DimensionSecurity),
		string(surface.CategoryIDOR),
		string(surface.CategoryAuthz),
		string(surface.CategoryOverfetch),
		string(surface.CategoryNPlus1),
	}
}

// openCache opens the run's finding cache, bound to the running analyzer's
// identity. It is a package variable, and the only reason is that two states the
// cache must handle correctly — a DIFFERENT analyzer binary, and an analyzer
// identity that cannot be resolved at all — cannot be produced from a test any
// other way, and both are correctness requirements rather than conveniences.
// Production never reassigns it.
var openCache = cache.Open

// skipDirs are directories never walked.
var skipDirs = map[string]bool{
	".git": true, "vendor": true, "node_modules": true,
	"dist": true, "bin": true, ".codefit": true,
}

// Run walks the project's source files and returns the security findings.
//
// Layer 0 of the pyramid (ctx.Scope) decides which of those files are ANALYSED.
// The walk itself is not narrowed: every auditable file is still counted, so the
// result can state "3 of 412" rather than the self-flattering "3 of 3". The gate
// is Narrows, not Includes — an AuditContext assembled without a scope audits
// everything (see the field doc); an unset scope must never blank an audit.
func (s *Sensor) Run(ctx auditctx.AuditContext) (findings.SensorResult, error) {
	start := time.Now()
	exts := s.provider.FileExtensions()
	narrowed := ctx.Scope.Narrows()
	fileCache := cacheFor(ctx)

	var all []findings.Finding
	var surface []findings.SurfaceItem
	var audited []string
	auditable := 0
	walkErr := filepath.WalkDir(ctx.ProjectRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !slices.Contains(exts, filepath.Ext(path)) {
			return nil
		}
		rel, relErr := filepath.Rel(ctx.ProjectRoot, path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		auditable++
		if narrowed && !ctx.Scope.Includes(rel) {
			return nil // layer 0: out of scope, never opened
		}
		audited = append(audited, rel)

		raw, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		// The cache is consulted here, on the RAW bytes, before anything is
		// parsed. A hit reuses the whole analysis; a miss, a corrupt entry or no
		// cache at all simply analyses the file. The file is still opened, still
		// counted and still reported either way — the cache decides what is
		// RECOMPUTED, never what is audited.
		key := ""
		if fileCache != nil {
			key = fileCache.Key(rel, raw)
		}
		fileFindings, fileSurface, hit := cached(fileCache, key)
		if !hit {
			var fileErr error
			fileFindings, fileSurface, fileErr = s.scanFile(rel, raw)
			if fileErr != nil {
				return fileErr
			}
			store(fileCache, key, rel, fileFindings, fileSurface)
		}
		// Criticality is applied HERE, on the way out, to a cached entry exactly
		// as to a fresh one: what is cached is the analysis, not its weighting.
		// An edit to path_criticality therefore re-weights the next scan without
		// invalidating a single entry.
		applyCriticality(ctx.Config, rel, fileFindings)
		all = append(all, fileFindings...)
		surface = append(surface, fileSurface...)
		return nil
	})
	if walkErr != nil {
		return findings.SensorResult{Sensor: s.Name(), Error: walkErr.Error()}, walkErr
	}
	sort.Strings(audited)

	return findings.SensorResult{
		Sensor:         s.Name(),
		Score:          scoring.DimensionScore(all),
		Findings:       all,
		Surface:        surface,
		AuditableTotal: auditable,
		AuditedFiles:   audited,
		DurationMs:     time.Since(start).Milliseconds(),
	}, nil
}

// cacheFor resolves the run's finding cache, or nil when there is to be none.
//
// It is nil in three cases, and the third is the one that matters: the project
// did not opt in (config.Cache.Enabled has no default, so a project with no
// cache: section has it off), there is no config at all, or the running
// analyzer's identity could not be resolved. An unknown analyzer identity means
// an unknown key input, and an unknown key input means DO NOT REUSE — codefit
// scans the whole project rather than guess which binary produced an entry.
//
// An empty cache.dir defaults to .codefit/cache inside the project: already
// gitignored, and already skipped by the walk.
func cacheFor(ctx auditctx.AuditContext) *cache.Cache {
	if ctx.Config == nil || !ctx.Config.Cache.Enabled {
		return nil
	}
	dir := ctx.Config.Cache.Dir
	if dir == "" {
		dir = filepath.Join(".codefit", "cache")
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(ctx.ProjectRoot, dir)
	}
	c, err := openCache(dir)
	if err != nil {
		slog.Warn("finding cache disabled for this run: the analyzer identity is unknown, "+
			"so no cached entry can be trusted; every file will be analysed",
			"dir", dir, "error", err)
		return nil
	}
	return c
}

// cached returns a file's stored analysis and whether it was found. No cache and
// no key are both plain misses, and so is an unreadable or corrupt entry: the
// cache may never be the reason a file is not analysed.
func cached(c *cache.Cache, key string) ([]findings.Finding, []findings.SurfaceItem, bool) {
	if c == nil || key == "" {
		return nil, nil, false
	}
	e, ok := c.Get(key)
	if !ok {
		return nil, nil, false
	}
	return e.Findings, e.Surface, true
}

// store records a file's analysis. An EMPTY result is stored like any other: a
// file with no findings and no surface is the ordinary case in a healthy
// repository, and re-analysing those forever would leave the cache doing nothing
// exactly where most of the work is.
//
// A failed write is a note, never a failed scan. The audit already happened; all
// that is lost is the saving on the next run.
func store(c *cache.Cache, key, rel string, fs []findings.Finding, surface []findings.SurfaceItem) {
	if c == nil || key == "" {
		return
	}
	if err := c.Set(key, cache.Entry{Findings: fs, Surface: surface}); err != nil {
		slog.Warn("finding cache write failed; the file was audited and reported normally, "+
			"only the saving on the next scan is lost", "file", rel, "error", err)
	}
}

// scanFile runs the pyramid layers over a single file's raw bytes: deterministic
// findings (regex + provider AST) and the mapped surface for the agent to reason
// about. codefit never runs a layer-3 LLM — the surface is returned to the agent.
//
// The bytes are read by the caller because the cache is keyed on them: reading
// the file twice to answer the same question is the kind of cost the cache exists
// to remove, not to add.
func (s *Sensor) scanFile(rel string, raw []byte) ([]findings.Finding, []findings.SurfaceItem, error) {
	// Bytes become TEXT before any layer sees them. A source file carrying a
	// byte-order mark — an ordinary thing in a repository a Windows tool has
	// touched — used to reach layer 1's regexes and the provider's AST as
	// NUL-interleaved bytes and yield ZERO findings at score 100, the same
	// false all-clear sensors/db carried for schema dumps. Decoding here rather
	// than in the provider keeps providers.SourceFile a text contract, and
	// keeps the fingerprints/snippets stamped below computed over exactly the
	// text the layers read.
	//
	// DECLARED RESIDUAL (ADR 0044): a BOM-LESS UTF-16 source file is still read
	// as-is and still yields nothing, silently. sourcetext.Decode refuses to
	// guess an encoding, and the source-level floor that turns that refusal
	// into a report exists only for the DB dimension's small, CONFIGURED source
	// list — a per-file note across a whole repository walk, where yielding no
	// finding is the ordinary case, would be noise rather than signal.
	content, _ := sourcetext.Decode(raw)
	src := providers.SourceFile{Path: rel, Content: content}
	var out []findings.Finding

	// Layer 1: language-agnostic regex for obvious secrets.
	out = append(out, layer1Secrets(rel, content)...)

	// Layer 2: provider AST analysis — deterministic findings + mapped surface.
	astFindings, err := s.provider.AnalyzeSecurity(src)
	if err != nil {
		return nil, nil, err
	}
	out = append(out, astFindings...)

	surface, err := s.provider.AnalyzeSurface(src)
	if err != nil {
		return nil, nil, err
	}

	deduped := dedupeFindings(out)
	stampFingerprints(rel, content, deduped, surface)
	return deduped, surface, nil
}

// stampFingerprints sets the baseline content identity of every finding and
// surface item. A finding is hashed from the SOURCE LINE at its position (findings
// carry no snippet); a surface item from its own snippet. The content is hashed,
// never stored, so a secret's line never reaches the committed baseline. This is
// the single boundary where identity is assigned, so no provider needs to know
// about the baseline.
func stampFingerprints(rel string, content []byte, fs []findings.Finding, surface []findings.SurfaceItem) {
	for i := range fs {
		if fs[i].Fingerprint == "" {
			// Include the rule ID in the identity: two distinct rules firing on the
			// SAME source line must never share a fingerprint, or accepting one would
			// silence the other — a deterministic affirmation suppressed without its
			// own human consent. The line itself stays OUT (move-robust, like surface).
			fs[i].Fingerprint = findings.Fingerprint(string(fs[i].Dimension)+"/"+fs[i].ID, rel, lineAt(content, fs[i].Line))
		}
	}
	for i := range surface {
		if surface[i].Fingerprint == "" {
			surface[i].Fingerprint = findings.Fingerprint(surface[i].Category, rel, surface[i].Snippet)
		}
	}
}

// lineAt returns the 1-based n-th line of content (empty when out of range). Used
// as the hashed content of a finding — it never leaves codefit unhashed.
func lineAt(content []byte, n int) string {
	if n <= 0 {
		return ""
	}
	lines := strings.Split(string(content), "\n")
	if n > len(lines) {
		return ""
	}
	return lines[n-1]
}

// dedupeFindings collapses the same finding reported twice — an identical
// (File, Line, ID) — keeping the highest-severity copy. The layer-1 regex
// (credential SHAPE) and a provider's layer-2 AST analysis (credential NAME)
// both emit SEC-001 and can fire on the same line (e.g. apiKey = "sk-ant-…");
// without this they would double-report. The criterion is deliberately STRICT:
// only an identical (File, Line, ID) is a duplicate. Different IDs on one line
// are different vulnerabilities and both survive — the sensor never decides that
// two distinct findings are "the same".
func dedupeFindings(in []findings.Finding) []findings.Finding {
	type key struct {
		file string
		line int
		id   string
	}
	seen := make(map[key]int, len(in)) // key -> index of the kept finding in out
	out := make([]findings.Finding, 0, len(in))
	for _, f := range in {
		k := key{f.File, f.Line, f.ID}
		if idx, ok := seen[k]; ok {
			if severityRank(f.Severity) > severityRank(out[idx].Severity) {
				out[idx] = f
			}
			continue
		}
		seen[k] = len(out)
		out = append(out, f)
	}
	return out
}

// severityRank orders severities so dedupeFindings keeps the most severe copy.
func severityRank(s findings.Severity) int {
	switch s {
	case findings.SeverityCritical:
		return 5
	case findings.SeverityHigh:
		return 4
	case findings.SeverityMedium:
		return 3
	case findings.SeverityLow:
		return 2
	case findings.SeverityInfo:
		return 1
	default:
		return 0
	}
}

// apiKeyPatterns matches well-known credential shapes (layer 1).
var apiKeyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-ant-[A-Za-z0-9-]{8,}`),      // Anthropic
	regexp.MustCompile(`sk-[A-Za-z0-9]{20,}`),          // OpenAI
	regexp.MustCompile(`ghp_[A-Za-z0-9]{20,}`),         // GitHub PAT
	regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`), // Slack
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),             // AWS access key id
}

// layer1Secrets scans raw text for credential-shaped strings.
func layer1Secrets(rel string, content []byte) []findings.Finding {
	var out []findings.Finding
	for _, line := range nonEmptyLines(content) {
		for _, re := range apiKeyPatterns {
			if re.MatchString(line.text) {
				out = append(out, findings.Finding{
					ID:              "SEC-001",
					Dimension:       findings.DimensionSecurity,
					Severity:        findings.SeverityCritical,
					File:            rel,
					Line:            line.number,
					Title:           "Possible hardcoded API key",
					Description:     "A string matching a known API-key format is present in the source.",
					Suggestion:      "Move the secret to an environment variable or the OS keychain.",
					Confidence:      1.0,
					RequiresConsent: true,
				})
				break // one finding per line is enough
			}
		}
	}
	return out
}

type sourceLine struct {
	number int
	text   string
}

func nonEmptyLines(content []byte) []sourceLine {
	var out []sourceLine
	for i, t := range strings.Split(string(content), "\n") {
		if strings.TrimSpace(t) != "" {
			out = append(out, sourceLine{number: i + 1, text: t})
		}
	}
	return out
}

// applyCriticality adjusts each finding's severity by the file's path
// criticality: test downgrades one level, example forces info (RF-11).
func applyCriticality(cfg *config.Config, rel string, fs []findings.Finding) {
	if cfg == nil {
		return
	}
	criticality := cfg.PathCriticalityFor(rel)
	for i := range fs {
		fs[i].Severity = adjustSeverity(fs[i].Severity, criticality)
		fs[i].RequiresConsent = fs[i].Dimension == findings.DimensionSecurity &&
			fs[i].Severity == findings.SeverityCritical
	}
}

func adjustSeverity(sev findings.Severity, criticality string) findings.Severity {
	switch criticality {
	case config.CriticalityTest:
		return downgrade(sev)
	case config.CriticalityExample:
		return findings.SeverityInfo
	default:
		return sev
	}
}

func downgrade(sev findings.Severity) findings.Severity {
	switch sev {
	case findings.SeverityCritical:
		return findings.SeverityHigh
	case findings.SeverityHigh:
		return findings.SeverityMedium
	case findings.SeverityMedium:
		return findings.SeverityLow
	case findings.SeverityLow:
		return findings.SeverityInfo
	default:
		return findings.SeverityInfo
	}
}
