package cli

import "github.com/spf13/cobra"

func newReviewCmd() *cobra.Command {
	var since string
	cmd := &cobra.Command{
		Use:   "review [path]",
		Short: "Run only the LLM code-review sensor",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return notImplemented(cmd)
		},
	}
	cmd.Flags().StringVar(&since, "since", "", "review only code changed since the given git ref")
	return cmd
}
