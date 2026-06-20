package cli

import "github.com/spf13/cobra"

func newBaselineCmd() *cobra.Command {
	var update bool
	cmd := &cobra.Command{
		Use:   "baseline",
		Short: "Snapshot current findings as historical debt; report only new ones afterwards",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return notImplemented(cmd)
		},
	}
	cmd.Flags().BoolVar(&update, "update", false, "regenerate the baseline snapshot (e.g. after paying down debt)")
	return cmd
}
