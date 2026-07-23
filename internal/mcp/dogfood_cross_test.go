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
//	   machine, never committed), listing the projects you have locally:
//
//	     [
//	       {"name":"node-express","root":"/abs/clone","schema":"prisma/schema.prisma"},
//	       {"name":"salonpro","root":"/abs/salonpro","schema":"app/frontend/prisma/schema.prisma"}
//	     ]
//
//	   (override the config path with CODEFIT_DOGFOOD_CONFIG=/some/file.json)
//	2. go test -tags dogfood -run TestDogfoodCross -v ./internal/mcp/
//
// THE THREE PROJECTS the PR #65 table cites:
//   - gothinkster/node-express-prisma-v1-official-app — Prisma RealWorld; git clone it.
//   - lujakob/nestjs-realworld-example-app — TypeORM, no .prisma → cross N/A.
//   - salonpro — a private ~40-model production Prisma schema.
//
// SKIP-IF-ABSENT: no config file → the whole test skips clean; a config entry whose
// root or schema file is not on this machine → that entry skips. Whoever has the
// fixtures measures; whoever does not breaks nothing (exit 0, no lie).
package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/codefit-cli/codefit/internal/config"
	"github.com/codefit-cli/codefit/internal/core/crossrules"
	"github.com/codefit-cli/codefit/internal/core/surface"
)

// dogfoodProject is one entry of dogfood.local.json.
type dogfoodProject struct {
	Name   string `json:"name"`
	Root   string `json:"root"`
	Schema string `json:"schema"` // schema path relative to Root
}

func dogfoodConfigPath() string {
	if p := os.Getenv("CODEFIT_DOGFOOD_CONFIG"); p != "" {
		return p
	}
	return "dogfood.local.json" // repo root (test cwd is the package dir, so climb)
}

func TestDogfoodCross(t *testing.T) {
	path := dogfoodConfigPath()
	// The test runs with cwd = the package dir; the default config lives at the repo
	// root, two levels up. An absolute CODEFIT_DOGFOOD_CONFIG is used verbatim.
	if !filepath.IsAbs(path) {
		path = filepath.Join("..", "..", path)
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
			if _, err := os.Stat(filepath.Join(p.Root, filepath.FromSlash(p.Schema))); err != nil {
				t.Skipf("schema absent (skip-if-absent): %s/%s", p.Root, p.Schema)
			}

			cfg := &config.Config{}
			cfg.Database.Type = "postgresql"
			cfg.Database.SchemaPaths = []string{p.Schema}

			_, res, ran := runDBForScanAll(p.Root, "typescript", cfg, crossrules.All())
			if !ran {
				t.Fatalf("db did not run (schema parse failed?) for %s", p.Name)
			}

			var d10, d13 []string
			for _, it := range res.Surface {
				line := fmt.Sprintf("  %s:%d  %v", it.File, it.Line, it.StructuralSignals)
				switch it.Category {
				case string(surface.CategoryDBFilteredColumnNoIndex):
					d10 = append(d10, line)
				case string(surface.CategoryDBNoCompositeIndex):
					d13 = append(d13, line)
				}
			}
			sort.Strings(d10)
			sort.Strings(d13)

			t.Logf("\n=== %s (%s) ===", p.Name, p.Root)
			t.Logf("DB-010 (filtered column, no index): %d item(s)", len(d10))
			for _, l := range d10 {
				t.Log(l)
			}
			t.Logf("DB-013 (multi-column, no composite index): %d item(s)", len(d13))
			for _, l := range d13 {
				t.Log(l)
			}
			t.Logf("cross total = %d ; db surface total across all rules = %d", len(d10)+len(d13), len(res.Surface))
		})
	}
}
