package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/codefit-cli/codefit/internal/version"
)

// renderVersion formats the build identity injected at link time.
func renderVersion(ver, commit, date string) string {
	return fmt.Sprintf("codefit %s (commit %s, built %s)", ver, commit, date)
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version, commit and build date",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), renderVersion(version.Version, version.Commit, version.BuildDate))
			return nil
		},
	}
}
