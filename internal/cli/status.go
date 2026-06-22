package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/codefit-cli/codefit/internal/version"
)

// systemStatus is the data behind `codefit status`. MCP-first: codefit manages
// no LLM, so there is no provider/model/api-key here. Knowledge-pack and OSV
// connectivity fields are added when those subsystems land (Phase C / Fase 1).
type systemStatus struct {
	Version     string
	ConfigFound bool
	ConfigPath  string
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// renderSystemStatus formats the diagnostic status for the terminal.
func renderSystemStatus(s systemStatus) string {
	var b strings.Builder
	fmt.Fprintf(&b, "codefit %s\n", s.Version)
	fmt.Fprintf(&b, "  .codefit.yaml:   %s (%s)\n", yesNo(s.ConfigFound), s.ConfigPath)
	return b.String()
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Diagnostics: version and .codefit.yaml presence",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			s := systemStatus{
				Version:     version.Version,
				ConfigFound: fileExists(globals.config),
				ConfigPath:  globals.config,
			}
			fmt.Fprint(cmd.OutOrStdout(), renderSystemStatus(s))
			return nil
		},
	}
}
