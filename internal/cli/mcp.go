package cli

import "github.com/spf13/cobra"

func newMCPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "MCP server mode",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newMCPServeCmd())
	return cmd
}

func newMCPServeCmd() *cobra.Command {
	var port int
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the MCP server (stdio by default; --port for HTTP/SSE)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return notImplemented(cmd)
		},
	}
	cmd.Flags().IntVar(&port, "port", 0, "serve over HTTP/SSE on this port instead of stdio")
	return cmd
}
