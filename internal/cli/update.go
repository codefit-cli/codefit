package cli

import "github.com/spf13/cobra"

func newUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Download the latest knowledge packs (framework best-practice rules)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return notImplemented(cmd)
		},
	}
}
