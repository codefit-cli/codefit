package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/codefit-cli/codefit/internal/mcp"
)

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
		Short: "Start the MCP server (stdio; --port for HTTP/SSE is not implemented yet)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if port != 0 {
				return fmt.Errorf("the HTTP/SSE transport (--port) is not implemented yet; use stdio (no --port)")
			}
			return mcp.Serve(cmd.Context())
		},
	}
	cmd.Flags().IntVar(&port, "port", 0, "serve over HTTP/SSE on this port instead of stdio (not implemented yet)")
	return cmd
}
