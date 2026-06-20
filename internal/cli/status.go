package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/codefit-cli/codefit/internal/version"
)

// systemStatus is the data behind `codefit status`.
type systemStatus struct {
	Version         string
	Provider        string
	Model           string
	KeyConfigured   bool
	DockerAvailable bool
	ConfigFound     bool
	ConfigPath      string
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// renderSystemStatus formats the system status for the terminal.
func renderSystemStatus(s systemStatus) string {
	var b strings.Builder
	fmt.Fprintf(&b, "codefit %s\n", s.Version)
	fmt.Fprintf(&b, "  provider:        %s\n", orDash(s.Provider))
	fmt.Fprintf(&b, "  model:           %s\n", orDash(s.Model))
	fmt.Fprintf(&b, "  api key:         %s\n", configuredState(s.KeyConfigured))
	fmt.Fprintf(&b, "  docker:          %s\n", yesNo(s.DockerAvailable))
	fmt.Fprintf(&b, "  .codefit.yaml:   %s (%s)\n", yesNo(s.ConfigFound), s.ConfigPath)
	return b.String()
}

// renderAuthStatus formats `codefit auth status`. It receives only a boolean
// for the key, so it can never print the credential itself.
func renderAuthStatus(provider, model string, keyConfigured bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "provider: %s\n", orDash(provider))
	fmt.Fprintf(&b, "model:    %s\n", orDash(model))
	fmt.Fprintf(&b, "api key:  %s\n", configuredState(keyConfigured))
	return b.String()
}

func configuredState(ok bool) string {
	if ok {
		return "configured"
	}
	return "not found"
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show active configuration: provider, model, version, Docker availability",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, g, err := loadGlobalConfig()
			if err != nil {
				return err
			}
			s := systemStatus{
				Version:         version.Version,
				Provider:        g.Provider,
				Model:           g.Model,
				KeyConfigured:   credentialConfigured(g),
				DockerAvailable: dockerAvailable(),
				ConfigFound:     fileExists(globals.config),
				ConfigPath:      globals.config,
			}
			fmt.Fprint(cmd.OutOrStdout(), renderSystemStatus(s))
			return nil
		},
	}
}
