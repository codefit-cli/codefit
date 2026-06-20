package cli

import (
	"fmt"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/codefit-cli/codefit/internal/auth"
)

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication with LLM providers",
		Args:  cobra.NoArgs,
	}
	cmd.AddCommand(newAuthLoginCmd(), newAuthLogoutCmd(), newAuthStatusCmd())
	return cmd
}

func newAuthLoginCmd() *cobra.Command {
	var provider string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Configure an LLM provider (interactive wizard)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			path, err := globalConfigPath()
			if err != nil {
				return err
			}
			return auth.RunLogin(auth.NewStore(), path, provider)
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "", "go straight to this provider (e.g. anthropic, openai, ollama)")
	return cmd
}

func newAuthLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove stored credentials from the keychain",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, g, err := loadGlobalConfig()
			if err != nil {
				return err
			}
			if g.Provider == "" {
				fmt.Fprintln(cmd.OutOrStdout(), "No provider configured; nothing to remove.")
				return nil
			}
			confirm := false
			if err := huh.NewConfirm().
				Title(fmt.Sprintf("Remove the stored %s credential?", g.Provider)).
				Value(&confirm).
				Run(); err != nil {
				return err
			}
			if !confirm {
				fmt.Fprintln(cmd.OutOrStdout(), "Cancelled.")
				return nil
			}
			if err := auth.NewStore().Delete(g.Provider); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "✓ Removed %s credential.\n", g.Provider)
			return nil
		},
	}
}

func newAuthStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the active provider and model",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, g, err := loadGlobalConfig()
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), renderAuthStatus(g.Provider, g.Model, credentialConfigured(g)))
			return nil
		},
	}
}
