//go:build dogfood

// Dogfood harness for SEC-001 over real projects (issue #148): how many SEC-001
// each provider reports on each project, after the value veto. It is the field
// half of the value-axis work; the table tests in internal/core/valueshape and
// in both providers prove the rule on fixtures, and this file counts what the
// rule does to a real tree.
//
// It counts through sec001FieldCounts (sec001_fieldtest_test.go, untagged), so
// the counter is the one TestSEC001FieldCountsSeeBothProviders locks on every
// ordinary `go test ./...`, and it runs the same planted control first: the
// numbers below come from a counter shown alive in the same run.
//
// READ-ONLY OVER THE PROJECTS. The configuration is built in memory, with no
// finding cache, and the project's own .codefit.yaml is never read — which is
// why this calls the sensor and not runSecurity or buildScanAll: those load the
// project's .codefit.yaml, and one that enables the cache with a relative
// directory would make the sensor write it inside the project.
//
// COUNTS ONLY. The -v output of this test is meant to be pasted into public
// places, so it prints a project's name (from the config file), how many files
// each provider audited, how many it did not parse whole, how many SEC-001 it
// reported and how long it took — never a path, a file name, a line or a matched
// text. A sensor failure is reported by language only, because its message can
// name a file. If you need to look at the findings behind a number, do it in a
// scratch copy and keep what you see out of the repository: describe the form,
// never the identifier (CLAUDE.md, "Dogfood sobre proyectos ajenos"). A config
// entry's name is what gets printed, so name an entry after what you are free to
// publish.
//
// Build-tagged `dogfood`, skip-if-absent, the config format of
// dogfood_cross_test.go ("schema" is ignored here):
//
//	CODEFIT_DOGFOOD_CONFIG=/abs/dogfood.json \
//	  go test -tags dogfood -count=1 -run TestDogfoodSEC001FieldCounts -v ./internal/mcp/
package mcp

import (
	"errors"
	"os"
	"testing"
)

// TestDogfoodSEC001FieldCounts reports, per configured project and per provider,
// the files audited, the files not parsed whole and the SEC-001 count. A
// provider that audits no file in a project is skipped for that project (it is
// not that project's language); a project where NO provider audits a file fails,
// because a row of zeros there would be a pass by vacuum.
func TestDogfoodSEC001FieldCounts(t *testing.T) {
	requireSEC001FieldControl(t)
	projects := loadDogfoodProjects(t)

	type row struct {
		project string
		counts  []sec001FieldCount
	}
	var table []row
	for _, p := range projects {
		t.Run(p.Name, func(t *testing.T) {
			if _, err := os.Stat(p.Root); err != nil {
				t.Skip("root absent on this machine (skip-if-absent)")
			}
			counts, err := sec001FieldCounts(p.Root)
			if err != nil {
				var fe *sec001FieldError
				if errors.As(err, &fe) {
					t.Fatalf("the %s security sensor failed; its message is withheld because it can name a file", fe.language)
				}
				t.Fatal("the security sensor failed; its message is withheld because it can name a file")
			}
			var measured []sec001FieldCount
			for _, c := range counts {
				if c.FilesAudited == 0 {
					t.Logf("%-10s audits no file here, skipped", c.Language)
					continue
				}
				t.Logf("%s", c.summary())
				measured = append(measured, c)
			}
			if len(measured) == 0 {
				t.Fatal("no provider audited a single file of this project: its counts would be zeros measured over nothing")
			}
			table = append(table, row{project: p.Name, counts: measured})
		})
	}
	if len(table) == 0 {
		t.Skip("no configured project was measured on this machine")
	}
	t.Log("SEC-001 field counts (counts only):")
	for _, r := range table {
		for _, c := range r.counts {
			t.Logf("  %-16s %s", r.project, c.summary())
		}
	}
}
