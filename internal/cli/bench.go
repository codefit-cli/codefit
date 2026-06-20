package cli

import "github.com/spf13/cobra"

func newBenchCmd() *cobra.Command {
	var (
		function string
		dryRun   bool
		nValues  string
	)
	cmd := &cobra.Command{
		Use:   "bench [path]",
		Short: "Run complexity benchmarks in an ephemeral Docker sandbox",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return notImplemented(cmd)
		},
	}
	f := cmd.Flags()
	f.StringVar(&function, "function", "", "run only the benchmark with this id from the config")
	f.BoolVar(&dryRun, "dry-run", false, "build the harness but do not execute it")
	f.StringVar(&nValues, "n-values", "", "override the input sizes, e.g. 10,100,1000,10000")
	return cmd
}
