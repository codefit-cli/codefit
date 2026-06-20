package cli

import (
	"fmt"
	"log/slog"
	"os"
	"slices"
	"time"

	"github.com/spf13/cobra"

	"github.com/codefit-cli/codefit/internal/config"
	auditctx "github.com/codefit-cli/codefit/internal/core/context"
	"github.com/codefit-cli/codefit/internal/core/findings"
	"github.com/codefit-cli/codefit/internal/core/report"
	"github.com/codefit-cli/codefit/internal/core/scoring"
	"github.com/codefit-cli/codefit/internal/providers"
	"github.com/codefit-cli/codefit/internal/providers/golang"
	"github.com/codefit-cli/codefit/internal/sensors"
	"github.com/codefit-cli/codefit/internal/sensors/security"
	"github.com/codefit-cli/codefit/internal/version"
)

func newScanCmd() *cobra.Command {
	var (
		since   string
		sensorF []string
	)
	cmd := &cobra.Command{
		Use:   "scan [path]",
		Short: "Run all static sensors (no Docker required)",
		Long: "Run the static sensors over the project (or a path). Honors the global\n" +
			"--no-llm, --output, --fail-on and --quiet flags.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runScan(cmd, args, sensorF)
		},
	}
	f := cmd.Flags()
	f.StringVar(&since, "since", "", "only analyze files changed since the given git ref (e.g. HEAD~1, origin/main)")
	f.StringArrayVar(&sensorF, "sensor", nil, "run only this sensor (repeatable): security | review | db | complexity | practices | tests")
	return cmd
}

func runScan(cmd *cobra.Command, args, sensorFilter []string) error {
	cfg, err := config.Load(globals.config)
	if err != nil {
		return fmt.Errorf("loading config (run `codefit init` first?): %w", err)
	}

	root := "."
	if len(args) == 1 {
		root = args[0]
	}

	for _, notice := range scanNotices(globalSince(cmd)) {
		slog.Warn(notice)
	}

	ctx := auditctx.AuditContext{
		ProjectRoot: root,
		Language:    cfg.Project.Language,
		Framework:   cfg.Project.Framework,
		Config:      cfg,
		Since:       globalSince(cmd),
		NoLLM:       globals.noLLM,
		FailOn:      globals.failOn,
		Interactive: report.IsTTY(os.Stdout) && !globals.noTUI,
	}

	active, err := selectSensors(cfg.Project.Language, sensorFilter)
	if err != nil {
		return err
	}

	var results []findings.SensorResult
	for _, s := range active {
		res, err := s.Run(ctx)
		if err != nil {
			return fmt.Errorf("sensor %q failed: %w", s.Name(), err)
		}
		results = append(results, res)
	}

	ar := assembleReport(cfg, results, version.Version, time.Now())

	if err := writeReportJSON(ar, defaultReportPath); err != nil {
		return err
	}
	renderer := report.ChooseRenderer(globals.output, ctx.Interactive, globals.noTUI)
	out := cmd.OutOrStdout()
	if globals.outFile != "" {
		file, err := os.Create(globals.outFile)
		if err != nil {
			return fmt.Errorf("creating output file %q: %w", globals.outFile, err)
		}
		if err := renderer.Render(file, ar); err != nil {
			_ = file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
	} else if err := renderer.Render(out, ar); err != nil {
		return err
	}

	if shouldFail(ar, globals.failOn) {
		return fmt.Errorf("audit failed: findings at or above --fail-on=%s (or a blocking critical security finding)", globals.failOn)
	}
	return nil
}

// scanNotices returns user-facing warnings about scan flags that are accepted
// but not yet effective, so they never silently mislead. Incremental --since
// filtering arrives in Phase 1 (PRD RF-08); until then a full scan runs.
func scanNotices(since string) []string {
	var notices []string
	if since != "" {
		notices = append(notices,
			"--since is not active yet (incremental scanning lands in Phase 1); running a full scan")
	}
	return notices
}

// severityRank orders severities for --fail-on comparisons.
var severityRank = map[findings.Severity]int{
	findings.SeverityInfo:     0,
	findings.SeverityLow:      1,
	findings.SeverityMedium:   2,
	findings.SeverityHigh:     3,
	findings.SeverityCritical: 4,
}

// shouldFail reports whether the run must exit non-zero: a blocking report
// (critical security without consent) always fails; otherwise it fails when any
// finding reaches the --fail-on threshold.
func shouldFail(ar report.AuditReport, failOn string) bool {
	if ar.Blocked {
		return true
	}
	threshold, ok := severityRank[findings.Severity(failOn)]
	if !ok {
		return false
	}
	for _, f := range ar.Findings {
		if severityRank[f.Severity] >= threshold {
			return true
		}
	}
	return false
}

// globalSince reads the scan-local --since flag (kept separate from the global
// flags struct).
func globalSince(cmd *cobra.Command) string {
	v, _ := cmd.Flags().GetString("since")
	return v
}

// resolveProvider returns the language provider for a language.
func resolveProvider(language string) (providers.LanguageProvider, error) {
	switch language {
	case "go":
		return golang.New(), nil
	default:
		return nil, fmt.Errorf("no provider for language %q (supported: go)", language)
	}
}

// selectSensors builds the sensors to run, honoring the --sensor filter. Only
// the security sensor exists today.
func selectSensors(language string, filter []string) ([]sensors.Sensor, error) {
	provider, err := resolveProvider(language)
	if err != nil {
		return nil, err
	}
	all := []sensors.Sensor{security.New(provider)}

	if len(filter) == 0 {
		return all, nil
	}
	var selected []sensors.Sensor
	for _, s := range all {
		if slices.Contains(filter, s.Name()) {
			selected = append(selected, s)
		}
	}
	return selected, nil
}

// assembleReport turns sensor results into the canonical AuditReport, computing
// scores and the deploy-block decision.
func assembleReport(cfg *config.Config, results []findings.SensorResult, ver string, ts time.Time) report.AuditReport {
	var all []findings.Finding
	for _, r := range results {
		all = append(all, r.Findings...)
	}

	blocked := scoring.IsBlocked(all)
	blockReason := ""
	if blocked {
		blockReason = "critical security findings without explicit consent"
	}

	return report.AuditReport{
		SchemaVersion:  report.SchemaVersion,
		CodefitVersion: ver,
		Timestamp:      ts,
		Project:        cfg.Project.Name,
		Language:       cfg.Project.Language,
		Score:          scoring.Compute(all, resolveWeights(cfg)),
		Blocked:        blocked,
		BlockReason:    blockReason,
		Findings:       all,
		SensorResults:  results,
	}
}

// resolveWeights returns the per-dimension weights from config, or the defaults.
func resolveWeights(cfg *config.Config) map[findings.Dimension]int {
	if len(cfg.Report.ScoreWeights) == 0 {
		return scoring.DefaultWeights()
	}
	w := make(map[findings.Dimension]int, len(cfg.Report.ScoreWeights))
	for k, v := range cfg.Report.ScoreWeights {
		w[findings.Dimension(k)] = v
	}
	return w
}

// writeReportJSON writes the canonical JSON report to path.
func writeReportJSON(ar report.AuditReport, path string) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating report file %q: %w", path, err)
	}
	if err := (report.JSONRenderer{}).Render(file, ar); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
