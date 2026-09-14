package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/atheory-ai/skillex/internal/capability"
	"github.com/atheory-ai/skillex/internal/config"
	"github.com/atheory-ai/skillex/internal/trust"
	"github.com/spf13/cobra"
)

type authStatusEntry struct {
	Server       string                        `json:"server"`
	Version      string                        `json:"version"`
	Profile      string                        `json:"profile,omitempty"`
	Scope        string                        `json:"scope"`
	Availability capability.AvailabilityStatus `json:"availability"`
}

func newAuthCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Inspect downstream MCP authentication readiness"}
	cmd.AddCommand(newAuthStatusCmd(), newAuthLoginCmd())
	return cmd
}

func newAuthLoginCmd() *cobra.Command {
	var profile, callback string
	cmd := &cobra.Command{
		Use: "login", Short: "Start or complete an explicit OAuth authorization-code login",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if profile == "" {
				return fmt.Errorf("--profile is required")
			}
			trusted, _, err := trust.LoadConfigured()
			if err != nil {
				return err
			}
			if callback != "" {
				if err := trusted.CompleteOAuthLogin(cmd.Context(), profile, callback); err != nil {
					return err
				}
				if flagJSON {
					return json.NewEncoder(os.Stdout).Encode(map[string]any{"type": "complete", "profile": profile})
				}
				fmt.Printf("OAuth login complete for %s\n", profile)
				return nil
			}
			authorizationURL, err := trusted.StartOAuthLogin(cmd.Context(), profile)
			if err != nil {
				return err
			}
			if flagJSON {
				return json.NewEncoder(os.Stdout).Encode(map[string]any{
					"type": "authorization_required", "profile": profile,
					"authorization_url": authorizationURL,
					"next_action":       "open the URL, then rerun with --callback '<full redirected URL>'",
				})
			}
			fmt.Printf("Open this URL:\n%s\n\nThen run:\nskillex auth login --profile %s --callback '<full redirected URL>'\n", authorizationURL, profile)
			return nil
		},
	}
	cmd.Flags().StringVar(&profile, "profile", "", "Trusted authorization-code credential profile")
	cmd.Flags().StringVar(&callback, "callback", "", "Full redirect URL received after authorization")
	return cmd
}

func newAuthStatusCmd() *cobra.Command {
	var serverFilter, profileFilter string
	cmd := &cobra.Command{
		Use: "status", Short: "Show credential readiness without revealing or testing credential values",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := repoRoot()
			cfg, err := config.Load(root)
			if err != nil {
				return err
			}
			if !cfg.MCPEnabled() {
				return fmt.Errorf("MCP capability brokering is not enabled for this project")
			}
			trusted, _, err := trust.LoadConfigured()
			if err != nil {
				return err
			}
			var entries []authStatusEntry
			for _, binding := range cfg.MCP.Bindings {
				if serverFilter != "" && binding.Server != serverFilter || profileFilter != "" && binding.AuthProfile != profileFilter {
					continue
				}
				selected := capability.Capability{
					Server: capability.ServerVersion{Identity: capability.ServerIdentity{CanonicalName: binding.Server}, Version: binding.Version},
					Kind:   capability.CapabilityTool, Name: "readiness", RoutingScope: binding.Scope, AuthProfile: binding.AuthProfile,
				}
				entries = append(entries, authStatusEntry{
					Server: binding.Server, Version: binding.Version, Profile: binding.AuthProfile,
					Scope: binding.Scope, Availability: trusted.Ready(selected, root),
				})
			}
			if flagJSON {
				encoder := json.NewEncoder(os.Stdout)
				encoder.SetIndent("", "  ")
				return encoder.Encode(map[string]any{"bindings": entries})
			}
			for _, entry := range entries {
				profile := "public"
				if entry.Profile != "" {
					profile = entry.Profile
				}
				fmt.Printf("%s@%s  %s  %s  %s\n", entry.Server, entry.Version, profile, entry.Scope, entry.Availability)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&serverFilter, "server", "", "Filter by exact canonical server identity")
	cmd.Flags().StringVar(&profileFilter, "profile", "", "Filter by trusted credential profile")
	return cmd
}
