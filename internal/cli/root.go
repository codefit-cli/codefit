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
	quiet   bool
	verbose bool
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
		Short: "The MCP-first auditor for AI-generated code",
		Long: "codefit audits AI-generated code for what the developer never sees:\n" +
			"security vulnerabilities, structural DB problems, regression risk and more.\n" +
			"It runs exclusively as an MCP server that AI agents consume as tools; this\n" +
			"binary only exposes plumbing commands (mcp serve, init, status) — there is\n" +
			"no audit CLI and codefit never calls an LLM.",
		Version:       version.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			configureLogging()
		},
	}

	pf := root.PersistentFlags()
	pf.StringVar(&globals.config, "config", "./.codefit.yaml", "path to the project .codefit.yaml")
	pf.BoolVar(&globals.quiet, "quiet", false, "quiet logging (warnings and errors only)")
	pf.BoolVar(&globals.verbose, "verbose", false, "verbose, debug-level logging")

	root.AddCommand(
		newInitCmd(),
		newMCPCmd(),
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
