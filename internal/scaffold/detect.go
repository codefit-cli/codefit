package scaffold

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/codefit-cli/codefit/internal/config"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/registry"
	"github.com/codefit-cli/codefit/internal/schemasource"
)

// ProjectInfo is the deterministic picture of a project that drives config and
// skill generation. Every field is inferred from files on disk — never an LLM.
type ProjectInfo struct {
	Name            string // the project directory's base name
	Language        string // typescript | go | python | java
	Framework       string // a value within config.allowedFrameworks, or ""
	ORM             string // prisma | drizzle | typeorm | ""
	DBType          string // postgresql | mysql | sqlite | "" (within config.allowedDBTypes)
	DBParadigm      string // oltp | olap | mixed | "" (auto when a DB is detected, seeding paradigm detection)
	SchemaPaths     []string
	RouteHandlers   int // count of route handlers found (informational, for the report)
	PathCriticality config.PathCriticality
	// SchemaCandidates is every directory discovery FOUND holding SQL DDL at its
	// own level, with what the real parser made of it. At most one of them is
	// ever promoted into SchemaPaths (R4); the rest exist so the generated config
	// and the report can name the real paths codefit looked at and say why each
	// was not written live. An empty slice means codefit looked and found none —
	// which is a different fact from "codefit did not look".
	SchemaCandidates []SchemaCandidate
}

// SchemaCandidate is one directory discovery found, and the MEASUREMENT codefit
// made of it. Nothing here is a guess: Tables is what the real SQL-DDL parser
// reconstructed, and Reason states which gate the directory failed.
type SchemaCandidate struct {
	Path   string // project-relative, slash-spelled, exactly as it would be written
	Tables int    // tables the real parser reconstructed (0 when it never ran)
	Reason string // why it was not written live; "" when it proved
}

// Proven reports whether this candidate cleared BOTH gates codefit requires
// before a path may become a live schema_paths entry: its apply order is proven,
// and the real parser reconstructed at least one table from it.
func (c SchemaCandidate) Proven() bool { return c.Reason == "" && c.Tables > 0 }

// Detected reports whether Detect resolved a real auditable language provider
// for this project, i.e. whether Language names a language rather than
// config.LanguageUndetected.
//
// It is a METHOD over Language, deliberately not a `Detected bool` field: a
// field would be a second source of truth that can disagree with Language, and
// every renderer branches on this one predicate rather than comparing against a
// string literal it could misspell.
//
// The empty string counts as not detected too. Detect itself never produces it,
// but ProjectInfo is exported and RenderSkill/RenderConfig are exported, so a
// zero-valued struct reaches them. Rendering the code-scanning artifacts with
// `language: ""` baked into every copy-paste example is the same fabrication as
// the `"" → typescript` fallback this replaced, one shade quieter.
func (i ProjectInfo) Detected() bool {
	return i.Language != "" && i.Language != config.LanguageUndetected
}

// dirsToSkip are never walked when counting route handlers — vendored or built
// output, not the project's own source.
var dirsToSkip = map[string]bool{
	"node_modules": true, ".git": true, "dist": true, "build": true,
	".next": true, "vendor": true, "out": true,
}

// buildOutputDirs are the directories a BUILD writes, as opposed to the ones a
// developer writes. Schema discovery must not look inside them, and the reason
// is a correctness finding rather than tidiness.
//
// Maven copies src/main/resources/** into target/classes/**. A Flyway project
// that has been compiled therefore holds two directories with byte-identical
// proven DDL — and under R4 two proven candidates mean NO live block at all. The
// single most common shape this whole change exists to serve would break on
// every project that has ever run `mvn package`, and the developer would be told
// codefit cannot tell which of two copies is the schema. That answer is
// incomprehensible when one of them is a build artifact.
//
// De-duplicating twins by identical content was rejected instead: it cannot say
// WHICH copy is the source without a policy, and picking one is exactly the
// guess this change forbids. Skipping build output is a fact about build
// systems; choosing between twins is not.
var buildOutputDirs = map[string]bool{
	"target": true, "bin": true, "obj": true, ".gradle": true,
	".venv": true, "venv": true, "__pycache__": true, ".tox": true,
	"coverage": true, "tmp": true,
}

// fixtureDirs hold TEST DATA, not the project's schema. Go's own toolchain
// treats testdata as invisible to a build, and pruning it is the same kind of
// fact as pruning Maven's target: a statement about where a toolchain puts
// things, not a judgement about which copy is real.
//
// This is not hypothetical tidiness. codefit dogfoods `init` on ITSELF, and
// without this the walk found five candidates under testdata, proved exactly one
// of them — a two-table Flyway fixture written to exercise the DB sensor — and
// promoted it into codefit's own .codefit.yaml as codefit's schema. Exactly one
// proved, so R4's ambiguity rule was silent, and the answer was a confident,
// clean, wrong one.
//
// The trade is DECLARED and deliberately asymmetric. A project keeping its real
// schema under testdata/ is now missed, and the config says codefit found none —
// honest, and correctable by hand. The alternative is auditing a fixture while
// reporting a measured schema, which is the confident wrong answer this whole
// change exists to remove. Absence is the safe direction (ADR 0072).
var fixtureDirs = map[string]bool{
	"testdata": true, "fixtures": true, "__fixtures__": true, "__snapshots__": true,
}

// discoverySkipDirs is dirsToSkip UNION buildOutputDirs UNION fixtureDirs,
// computed rather than listed. The union is the invariant: anything already too
// vendored or too built to count route handlers in is also too vendored or too
// built to take a schema from, so a name added to dirsToSkip must never have to
// be remembered here as well. TestDiscovery_SkipsEveryDirsToSkipName holds the
// relation through the real walk, not through the map literal.
var discoverySkipDirs = unionDirs(dirsToSkip, buildOutputDirs, fixtureDirs)

// sortedKeys returns a map's true-valued keys in a deterministic order.
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func unionDirs(sets ...map[string]bool) map[string]bool {
	out := map[string]bool{}
	for _, s := range sets {
		for name, skip := range s {
			if skip {
				out[name] = true
			}
		}
	}
	return out
}

// DiscoveryDepth bounds how deep schema discovery walks, counting directory
// levels below the project root (root itself is 0).
//
// DISCOVERY DEPTH IS NOT READ DEPTH. The scan-time resolver reads exactly ONE
// level of a configured directory (ADR 0072's DECLARED LIMIT), and this change
// does not touch that. This number bounds only the search for candidate
// directories, and candidacy is decided PER DIRECTORY, so discovery can never
// hand the resolver a path whose files live below the level it reads.
//
// 6 comes from the layout this change exists to serve. Flyway's canonical
// location on a JVM project is src/main/resources/db/migration — five levels. A
// bound of 3 or 4 would miss the single most common shape of the exact case
// codefit is trying to stop being blind to, which would be self-defeating; 6 is
// that depth plus one level of headroom. Deeper is not needed and would
// contradict artifacts codefit already ships: both the generated config and the
// generated skill instruct the developer to run `codefit init` per sub-project
// root, and language detection does not recurse at all.
//
// The bound is DECLARED, not silent: the generated config states it, so "no
// schema source found" can never be mistaken for "this project has none".
const DiscoveryDepth = 6

// Detect inspects root and infers the project's language and stack from marker
// files.
//
// It NEVER refuses over language. When no marker file resolves an auditable
// provider it returns config.LanguageUndetected with an EMPTY PathCriticality,
// and the caller declares that gap — it does not withhold the config. codefit
// informs; the developer decides. The refusal this replaced took a decision
// away over a field no sensor reads, and its message named four manifests that
// cannot resolve anything while omitting tsconfig.json, which can: it told a
// project holding a Java manifest to create that same Java manifest.
//
// Errors survive ONLY for genuine path/IO failures, and such an error must
// never name marker files — a list of manifests to create is unactionable when
// the real problem is the path.
//
// path_criticality is left empty rather than borrowed from some provider's
// defaults. Empty is the safe direction: nothing is classified "test", so
// RF-10's test-path re-weighting never fires and every finding keeps its
// natural severity. Inventing globs would silently downgrade findings in a tree
// codefit never inspected.
func Detect(root string) (ProjectInfo, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return ProjectInfo{}, fmt.Errorf("resolving project root %q: %w", root, err)
	}

	info := ProjectInfo{Name: filepath.Base(abs), Language: config.LanguageUndetected}

	// The language half returns early when nothing resolves; the SCHEMA half does
	// not, and must not. It used to: an unresolved provider ended Detect, so no
	// schema enrichment ran for an undetected root — which meant a Java service
	// with Flyway migrations, the exact project a language-independent SQL-DDL
	// parser was built for, got nothing. A schema source is orthogonal to the
	// application language (ADR 0018), so it is discovered on its own terms.
	if provider := detectLanguage(root); provider != nil {
		info.Language = provider.Language()
		info.PathCriticality = provider.DefaultPathCriticality()

		// Route-handler enrichment is a TypeScript/Next concern (route.ts files); for
		// other languages it would be a wasted full-tree walk that can only count zero.
		if info.Language == "typescript" {
			enrichTypeScript(root, &info)
			info.RouteHandlers = countRouteHandlers(root)
		}
	}
	discoverSQLSchema(root, &info)
	return info, nil
}

// Reasons a discovered directory was not written live. They are constants
// because two artifacts quote them — the generated config and the init report —
// and a reason that drifts between the two describes a codefit the developer
// does not have.
const (
	// ReasonOrderNotProven is the golang-migrate case, and every other naming
	// codefit cannot version.
	ReasonOrderNotProven = "codefit cannot prove the apply order of these filenames"
	// ReasonNoTable is an ordered directory the real parser read and made
	// nothing of.
	ReasonNoTable = "reconstructed no table"
	// ReasonAmbiguous is R4: more than one directory proved, and choosing
	// between them would be a guess.
	ReasonAmbiguous = "more than one directory proved"
)

// discoverSQLSchema finds SQL schema directories and promotes AT MOST ONE of
// them into info.SchemaPaths.
//
// It runs for EVERY project, detected or not. A schema source is orthogonal to
// the application language (ADR 0018) — a Java service with Flyway migrations is
// the case that motivated the whole change — so gating this on a resolved
// language provider would blind codefit to precisely the projects the SQL-DDL
// parser was built for.
//
// It does NOT run when a schema is already resolved. Prisma's detection measures
// its datasource provider and writes both schema_paths and a live type:; adding
// a second entry beside it would mix .prisma and .sql in one schema_paths, which
// resolves NO parser at all (ParserForPaths' declared limit). A project that
// already has a proven schema loses nothing by codefit not looking for a second
// one, and would lose the first one if it did.
func discoverSQLSchema(root string, info *ProjectInfo) {
	if len(info.SchemaPaths) > 0 {
		return
	}
	info.SchemaCandidates = detectSchemaCandidates(root)

	var proven []SchemaCandidate
	for _, c := range info.SchemaCandidates {
		if c.Proven() {
			proven = append(proven, c)
		}
	}
	if len(proven) == 1 {
		info.SchemaPaths = []string{proven[0].Path}
		return
	}
	// Zero proven: every candidate already carries its own reason. Two or more:
	// each proved, so none carries a reason yet — and the honest one is that
	// codefit cannot know whether they are one schema or several. Merging them
	// would poison one reconstructed model; picking the first would be the guess
	// the governing rule forbids.
	for i := range info.SchemaCandidates {
		if info.SchemaCandidates[i].Proven() {
			info.SchemaCandidates[i].Reason = ReasonAmbiguous
		}
	}
}

// detectSchemaCandidates walks root looking for directories that hold SQL DDL at
// their OWN level, and measures each one through the real parser.
//
// Candidacy is per DIRECTORY, never per subtree. db/nested/sql/V1__a.sql makes
// db/nested/sql a candidate and leaves db/nested — which holds no .sql at its
// own level — out entirely. Letting the outer directory inherit candidacy from a
// descendant is the exact confusion between discovery depth and read depth that
// would make init write a path the scan-time resolver reads as empty.
func detectSchemaCandidates(root string) []SchemaCandidate {
	var found []SchemaCandidate
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an unreadable entry is not a schema; keep walking
		}
		if !d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil //nolint:nilerr // unreachable under WalkDir; never abandon the walk
		}
		if rel == "." {
			return nil
		}
		if discoverySkipDirs[d.Name()] {
			return fs.SkipDir
		}
		slashed := filepath.ToSlash(rel)
		// The bound is checked BEFORE candidacy, so a directory past it is never
		// measured and never named — "not found" means exactly that.
		if strings.Count(slashed, "/")+1 > DiscoveryDepth {
			return fs.SkipDir
		}

		proof, proveErr := schemasource.Prove(root, slashed)
		if proveErr != nil {
			return nil //nolint:nilerr // this directory is not a schema; the rest of the tree still might be
		}
		if proof.SQLFiles == 0 {
			return nil
		}
		found = append(found, SchemaCandidate{
			Path:   slashed,
			Tables: proof.Tables,
			Reason: reasonFor(proof),
		})
		return nil
	})
	// WalkDir is already lexical, but the order reaches a committed file and a
	// printed report, so it is stated rather than inherited.
	sort.Slice(found, func(i, j int) bool { return found[i].Path < found[j].Path })
	return found
}

// reasonFor names the gate a discovered directory failed, or "" when it cleared
// both. It never invents a reason for a directory that proved: R4's ambiguity is
// a fact about the SET of candidates, not about any one of them, and is applied
// by the caller that can see them all.
func reasonFor(p schemasource.Proof) string {
	switch {
	case !p.Ordered:
		return ReasonOrderNotProven
	case p.Tables == 0:
		return ReasonNoTable
	default:
		return ""
	}
}

// detectLanguage resolves the project's language provider from marker files,
// via internal/providers/registry.ByMarkerFile — the registry's table order
// IS the priority (go.mod wins over package.json in a polyglot root; a
// monorepo with separate per-language sub-projects should run init per
// sub-project root rather than at the polyglot root). Only TypeScript and Go
// are registered today; Python and Java markers are recognized elsewhere
// (readPackageDeps et al.) but unsupported, so they fall through to nil.
func detectLanguage(root string) providers.LanguageProvider {
	e, ok := registry.ByMarkerFile(root)
	if !ok {
		return nil
	}
	return e.New(nil)
}

// enrichTypeScript fills framework, ORM and database from a TypeScript project's
// package.json and Prisma schema, and widens path_criticality for the framework's
// route location (Next.js handlers live under app/, which the src/** default misses).
func enrichTypeScript(root string, info *ProjectInfo) {
	deps := readPackageDeps(root)
	info.Framework = frameworkFromDeps(deps)

	if info.Framework == "next" && !slices.Contains(info.PathCriticality.Production, "app/**") {
		info.PathCriticality.Production = append(
			[]string{"app/**"}, info.PathCriticality.Production...)
	}

	if rel := findPrismaSchema(root); rel != "" {
		info.ORM = "prisma"
		info.SchemaPaths = []string{rel}
		info.DBParadigm = "auto"
		info.DBType = prismaProvider(filepath.Join(root, rel))
	} else if _, ok := deps["drizzle-orm"]; ok {
		info.ORM = "drizzle"
	} else if _, ok := deps["typeorm"]; ok {
		info.ORM = "typeorm"
	}
}

// frameworkFromDeps maps package.json dependencies to a framework recognized by
// config validation. next implies react, so it wins; nestjs is intentionally not
// emitted because it is not in config.allowedFrameworks (emitting it would produce
// an invalid .codefit.yaml).
func frameworkFromDeps(deps map[string]string) string {
	switch {
	case has(deps, "next"):
		return "next"
	case has(deps, "express"):
		return "express"
	case has(deps, "react"):
		return "react"
	default:
		return ""
	}
}

// readPackageDeps returns the merged dependencies + devDependencies of the
// project's package.json (empty map when absent or unparseable).
func readPackageDeps(root string) map[string]string {
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return map[string]string{}
	}
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return map[string]string{}
	}
	merged := make(map[string]string, len(pkg.Dependencies)+len(pkg.DevDependencies))
	for k, v := range pkg.Dependencies {
		merged[k] = v
	}
	for k, v := range pkg.DevDependencies {
		merged[k] = v
	}
	return merged
}

// findPrismaSchema returns the project-relative path of prisma/schema.prisma if
// present, in slash-normalized form for the config; "" otherwise.
func findPrismaSchema(root string) string {
	rel := filepath.Join("prisma", "schema.prisma")
	if exists(root, rel) {
		return filepath.FromSlash(rel)
	}
	return ""
}

// prismaDatasourceProvider extracts the datasource provider from a Prisma schema.
var prismaDatasourceProvider = regexp.MustCompile(`provider\s*=\s*"([a-zA-Z]+)"`)

// prismaProvider reads the datasource provider from a Prisma schema and maps it
// to a config-allowed DB type. Unknown providers (cockroachdb, mongodb, …) return
// "" rather than an invalid value.
func prismaProvider(schemaPath string) string {
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		return ""
	}
	// The first provider in the file is the datasource's (the generator's
	// provider is prisma-client-js, which the allow-list below excludes).
	for _, m := range prismaDatasourceProvider.FindAllStringSubmatch(string(data), -1) {
		switch m[1] {
		case "postgresql", "postgres":
			return "postgresql"
		case "mysql":
			return "mysql"
		case "sqlite":
			return "sqlite"
		}
	}
	return ""
}

// countRouteHandlers counts files named route.ts/route.tsx (the Next.js App
// Router convention) under root, skipping vendored and build directories. It is
// informational only — codefit's scanner discovers handlers itself at scan time.
func countRouteHandlers(root string) int {
	count := 0
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // skip unreadable entries, keep counting
		}
		if d.IsDir() {
			if dirsToSkip[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		if base := d.Name(); base == "route.ts" || base == "route.tsx" {
			count++
		}
		return nil
	})
	return count
}

func exists(root, rel string) bool {
	_, err := os.Stat(filepath.Join(root, rel))
	return err == nil
}

func has(deps map[string]string, name string) bool {
	_, ok := deps[name]
	return ok
}
