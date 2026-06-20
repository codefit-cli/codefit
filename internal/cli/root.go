package cli

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/codefit-cli/codefit/internal/version"
)

// globalFlags holds the root command's persistent flags, shared by every
// subcommand.
type globalFlags struct {
	config  string
	output  string
	outFile string
	failOn  string
	quiet   bool
	noLLM   bool
	verbose bool
	tui     bool
	noTUI   bool
}

var globals globalFlags

// Execute builds and runs the codefit command tree. It is the single entry
// point called from main.
func Execute() error {
	return newRootCmd().Execute()
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "codefit",
		Short: "Audit AI-generated code for what you never see",
		Long: "codefit is an open-source auditor for AI-generated code. It surfaces what a\n" +
			"developer never sees during normal development: security vulnerabilities,\n" +
			"algorithmic complexity that scales badly, structural DB problems, deep code\n" +
			"review issues and regression risk. It runs as a CLI or as a stateless MCP server.",
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			configureLogging()
		},
	}

	pf := root.PersistentFlags()
	pf.StringVar(&globals.config, "config", "./.codefit.yaml", "path to the project .codefit.yaml")
	pf.StringVar(&globals.output, "output", "markdown", "output format: json | markdown | html")
	pf.StringVar(&globals.outFile, "out-file", "", "write output to a file instead of stdout")
	pf.StringVar(&globals.failOn, "fail-on", "critical", "exit 1 on findings at this severity or worse: critical | high | medium")
	pf.BoolVar(&globals.quiet, "quiet", false, "only show the final score and critical findings")
	pf.BoolVar(&globals.noLLM, "no-llm", false, "disable analysis that requires an LLM")
	pf.BoolVar(&globals.verbose, "verbose", false, "verbose, debug-level logging")
	pf.BoolVar(&globals.tui, "tui", false, "force the interactive TUI renderer")
	pf.BoolVar(&globals.noTUI, "no-tui", false, "force the plain (non-interactive) renderer")
	root.MarkFlagsMutuallyExclusive("tui", "no-tui")

	root.AddCommand(
		newInitCmd(),
		newScanCmd(),
		newBenchCmd(),
		newReviewCmd(),
		newRunCmd(),
		newBaselineCmd(),
		newReportCmd(),
		newMCPCmd(),
		newAuthCmd(),
		newSetCmd(),
		newStatusCmd(),
	)
	return root
}

// configureLogging sets up the structured slog logger based on the global
// verbose/quiet flags.
func configureLogging() {
	level := slog.LevelInfo
	switch {
	case globals.verbose:
		level = slog.LevelDebug
	case globals.quiet:
		level = slog.LevelWarn
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(h))
}

// notImplemented is the shared stub for skeleton commands. It logs the call and
// prints a friendly notice so the command tree is fully navigable before the
// engine exists.
func notImplemented(cmd *cobra.Command) error {
	slog.Warn("command not implemented yet", "command", cmd.CommandPath())
	fmt.Fprintf(cmd.OutOrStdout(), "codefit %s: not implemented yet (scaffolding)\n", cmd.CommandPath())
	return nil
}
