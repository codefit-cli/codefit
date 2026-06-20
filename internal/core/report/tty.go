package report

import (
	"os"

	"github.com/mattn/go-isatty"
)

// IsTTY reports whether f is connected to an interactive terminal. codefit uses
// this to keep the plain renderer in pipes/CI/MCP and (later) enable the TUI
// only when interactive.
func IsTTY(f *os.File) bool {
	fd := f.Fd()
	return isatty.IsTerminal(fd) || isatty.IsCygwinTerminal(fd)
}
