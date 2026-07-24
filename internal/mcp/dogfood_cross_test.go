//go:build dogfood

// Dogfood harness for the code↔schema cross (DB-010 / DB-013). This is the
// instrument that caught the real problem of the index-vs-query phase: the
// table-driven tests verified correctness impeccably yet were blind to the cross
// eating 64% of the DB surface channel on a real schema. It turns "the rule is
// correct" into "the rule is useful", and every future cross rule measures from the
// baseline it reports — so it lives in the repo, not one `git clean` from gone.
//
// It runs the REAL production path (runDBForScanAll with crossrules.All(), no
// baseline write), building the config in memory so a dogfood project needs no
// committed .codefit.yaml.
//
// Build-tagged `dogfood`: EXCLUDED from the normal gate — CGO_ENABLED=0
// build/vet/test-race/lint never compile it.
//
// HOW TO RUN
//
//	1. Create dogfood.local.json at the repo root (gitignored — paths are per
//	   machine, never committed), listing the projects you have locally. "schema" is
//	   a path relative to "root" — either a single .prisma file OR a DIRECTORY of
//	   .prisma files (a multi-file Prisma schema):
//
//	     [
//	       {"name":"node-express","root":"/abs/clone","schema":"prisma/schema.prisma"},
//	       {"name":"papermark","root":"/abs/papermark","schema":"prisma/schema"}
//	     ]
//
//	   (override the config path with CODEFIT_DOGFOOD_CONFIG=/some/file.json)
//	2. go test -tags dogfood -run TestDogfoodCross -v ./internal/mcp/
//
// THE PROJECTS the PR #65 table cites (calibration N grows here):
//   - gothinkster/node-express-prisma-v1-official-app — Prisma RealWorld; git clone it.
//   - lujakob/nestjs-realworld-example-app — TypeORM, no .prisma → cross N/A.
//   - salonpro — a private ~40-model production Prisma schema.
//
// SKIP-IF-ABSENT: no config file → the whole test skips clean; a config entry whose
// root or schema is not on this machine → that entry skips. Whoever has the fixtures
// measures; whoever does not breaks nothing (exit 0, no lie).
package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	"github.com/codefit-cli/codefit/internal/core/crossrules"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// dogfoodProject is one entry of dogfood.local.json.
type dogfoodProject struct {
	Name   string `json:"name"`
	Root   string `json:"root"`
	Schema string `json:"schema"` // .prisma file OR a directory of .prisma files, relative to Root
}

func dogfoodConfigPath() string {
	if p := os.Getenv("CODEFIT_DOGFOOD_CONFIG"); p != "" {
		return p
	}
	return "dogfood.local.json"
}

// resolveSchemaPaths turns the config "schema" into the list of schema paths (root-
// relative) codefit parses: one file, or every .prisma under a directory (a
// multi-file Prisma schema — the parser is two-pass, so cross-file model refs
// resolve). Returns nil when nothing exists on this machine (→ skip).
func resolveSchemaPaths(root, schema string) []string {
	abs := filepath.Join(root, filepath.FromSlash(schema))
	info, err := os.Stat(abs)
	if err != nil {
		return nil
	}
	if !info.IsDir() {
		return []string{schema}
	}
	var out []string
	entries, _ := os.ReadDir(abs)
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".prisma") {
			out = append(out, filepath.ToSlash(filepath.Join(schema, e.Name())))
		}
	}
	sort.Strings(out)
	return out
}

func TestDogfoodCross(t *testing.T) {
	path := dogfoodConfigPath()
	if !filepath.IsAbs(path) {
		path = filepath.Join("..", "..", path) // test cwd is the package dir; config is at repo root
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no dogfood config (skip-if-absent): %v — see this file's header to create one", err)
	}
	var projects []dogfoodProject
	if err := json.Unmarshal(raw, &projects); err != nil {
		t.Fatalf("parsing dogfood config %s: %v", path, err)
	}
	if len(projects) == 0 {
		t.Skip("dogfood config is empty")
	}

	for _, p := range projects {
		t.Run(p.Name, func(t *testing.T) {
			if _, err := os.Stat(p.Root); err != nil {
				t.Skipf("root absent (skip-if-absent): %s", p.Root)
			}
			schemaPaths := resolveSchemaPaths(p.Root, p.Schema)
			if len(schemaPaths) == 0 {
				t.Skipf("schema absent (skip-if-absent): %s/%s", p.Root, p.Schema)
			}

			cfg := &config.Config{}
			cfg.Database.Type = "postgresql"
			cfg.Database.SchemaPaths = schemaPaths

			// Harness-side instrumentation (NOT the rule): the extracted query filters
			// by arity, so we can see how many the arity≥4 rule abstains on. This is the
			// PRE-reconcile column count (the raw WHERE columns); reconcile may drop a
			// relation/phantom column, so it is an upper bound on the arity cohort.
			provider := providerForLanguage("typescript", nil)
			ex, _ := provider.(providers.QueryExtractor)
			filters := collectQueryFilters(p.Root, provider.FileExtensions(), ex)
			var arity1, arity23, arity4plus int
			for _, f := range filters {
				switch n := len(f.Columns); {
				case n <= 1:
					arity1++
				case n <= 3:
					arity23++
				default:
					arity4plus++
				}
			}

			_, res, ran := runDBForScanAll(p.Root, "typescript", cfg, crossrules.All())
			if !ran {
				t.Fatalf("db did not run (schema parse failed?) for %s", p.Name)
			}

			var d10, d13 []string
			var d13Grouped, d13Single int
			for _, it := range res.Surface {
				line := fmt.Sprintf("  %s:%d  %v", it.File, it.Line, it.StructuralSignals)
				switch it.Category {
				case string(surface.CategoryDBFilteredColumnNoIndex):
					d10 = append(d10, line)
				case string(surface.CategoryDBNoCompositeIndex):
					d13 = append(d13, line)
					if affectedModelCount(it.StructuralSignals) > 1 {
						d13Grouped++
					} else {
						d13Single++
					}
				}
			}
			sort.Strings(d10)
			sort.Strings(d13)

			cross := len(d10) + len(d13)
			total := len(res.Surface)
			pct := 0.0
			if total > 0 {
				pct = 100 * float64(cross) / float64(total)
			}
			t.Logf("\n=== %s (%s) ===", p.Name, p.Root)
			t.Logf("query filters extracted: %d (1-col=%d, 2-3col=%d, 4+col[arity-abstained]=%d)", len(filters), arity1, arity23, arity4plus)
			t.Logf("DB-010: %d | DB-013: %d (grouped[>1 model]=%d, single=%d) | cross total=%d | db surface total=%d | cross %% of channel=%.0f%%",
				len(d10), len(d13), d13Grouped, d13Single, cross, total, pct)
			t.Log("--- DB-010 items ---")
			for _, l := range d10 {
				t.Log(l)
			}
			t.Log("--- DB-013 items ---")
			for _, l := range d13 {
				t.Log(l)
			}
		})
	}
}

// affectedModelCount reads the model count from a DB-013 item's "affected_models: A,
// B, C" signal (FIX 4 grouping).
func affectedModelCount(signals []string) int {
	for _, s := range signals {
		if rest, ok := strings.CutPrefix(s, "affected_models: "); ok {
			return len(strings.Split(rest, ", "))
		}
	}
	return 1
}
