package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/codefit-cli/codefit/internal/core/report"
)

// defaultReportPath is the canonical JSON report the `report` command renders
// when no input path is given.
const defaultReportPath = "codefit-report.json"

// renderReportFile loads a canonical JSON report from inPath and renders it to
// w in the requested format.
func renderReportFile(inPath, format string, w io.Writer) error {
	data, err := os.ReadFile(inPath)
	if err != nil {
		return fmt.Errorf("reading report %q: %w", inPath, err)
	}
	var ar report.AuditReport
	if err := json.Unmarshal(data, &ar); err != nil {
		return fmt.Errorf("parsing report %q: %w", inPath, err)
	}
	return report.ChooseRenderer(format, false, false).Render(w, ar)
}

func newReportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report [path]",
		Short: "Render the latest JSON result in the chosen format",
		Long:  "Render a canonical JSON report. Uses the global --output and --out-file flags.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			inPath := defaultReportPath
			if len(args) == 1 {
				inPath = args[0]
			}

			if globals.outFile == "" {
				return renderReportFile(inPath, globals.output, cmd.OutOrStdout())
			}
			file, err := os.Create(globals.outFile)
			if err != nil {
				return fmt.Errorf("creating output file %q: %w", globals.outFile, err)
			}
			if err := renderReportFile(inPath, globals.output, file); err != nil {
				_ = file.Close()
				return err
			}
			return file.Close()
		},
	}
	return cmd
}
