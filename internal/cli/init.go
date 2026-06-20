package cli

import "github.com/spf13/cobra"

func newInitCmd() *cobra.Command {
	var (
		nonInteractive bool
		force          bool
	)
	cmd := &cobra.Command{
		Use:   "init [path]",
		Short: "Analyze the project and generate an initial .codefit.yaml",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return notImplemented(cmd)
		},
	}
	f := cmd.Flags()
	f.BoolVar(&nonInteractive, "non-interactive", false, "skip the wizard and write sensible defaults")
	f.BoolVar(&force, "force", false, "overwrite an existing .codefit.yaml")
	return cmd
}
