// Command codefit is the CLI and MCP entry point for the codefit auditor.
package main

import (
	"fmt"
	"os"

	"github.com/codefit-cli/codefit/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "codefit:", err)
		os.Exit(1)
	}
}
