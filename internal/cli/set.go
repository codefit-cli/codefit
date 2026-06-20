package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/codefit-cli/codefit/internal/config"
)

func newSetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set",
		Short: "Configure global parameters (model, provider)",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newSetModelCmd())
	return cmd
}

// applyModelChange mutates the global config for a `set model` invocation,
// validating the resulting provider. Empty provider/url leave those fields
// untouched (except the model, which is always set).
func applyModelChange(g *config.GlobalConfig, name, provider, url string, local bool) error {
	g.Model = name
	if provider != "" {
		g.Provider = provider
	}
	if url != "" {
		g.BaseURL = url
	}
	_ = local // --local is a hint; locality is derived from the provider.
	return g.Validate()
}

func newSetModelCmd() *cobra.Command {
	var (
		local    bool
		url      string
		provider string
	)
	cmd := &cobra.Command{
		Use:   "model <name>",
		Short: "Set the active model (optionally switching provider)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, g, err := loadGlobalConfig()
			if err != nil {
				return err
			}
			if err := applyModelChange(g, args[0], provider, url, local); err != nil {
				return err
			}
			if err := config.SaveGlobal(path, g); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ Model set to %s (provider: %s)\n", g.Model, orDash(g.Provider))
			return nil
		},
	}
	f := cmd.Flags()
	f.BoolVar(&local, "local", false, "the model is served by a local provider (Ollama, LM Studio)")
	f.StringVar(&url, "url", "", "base URL of the local provider")
	f.StringVar(&provider, "provider", "", "switch to this provider as well")
	return cmd
}
