package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atheory-ai/skillex/internal/config"
	"github.com/atheory-ai/skillex/internal/packregistry"
	"github.com/atheory-ai/skillex/internal/packs"
	"github.com/atheory-ai/skillex/internal/registry"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newPackCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "pack", Short: "Manage cryptographically verified registry packs"}
	var yes, preview bool
	get := &cobra.Command{Use: "get <name>", Short: "Verify, preview, and install a registry pack", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		root := repoRoot()
		cfg, err := config.Load(root)
		if err != nil {
			return err
		}
		prepared, err := packregistry.Prepare(cmd.Context(), packregistry.ManifestURL(cfg), args[0])
		if err != nil {
			return err
		}
		proposal, err := packs.Preview(prepared)
		if err != nil {
			return err
		}
		if !flagQuiet {
			fmt.Fprintf(os.Stderr, "Verified %s@%s [%s] from %s\n", proposal.Pack.Name, proposal.Pack.Version, prepared.Lock.Entry.Tier, prepared.Lock.Registry)
			for _, name := range proposal.Files {
				fmt.Fprintf(os.Stderr, "  + %s\n", name)
			}
			activation, err := yaml.Marshal(map[string]interface{}{"skills": proposal.Skills, "detectors": proposal.Detectors, "mcp-servers": proposal.MCPServers})
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr, string(activation))
			if !preview {
				fmt.Fprintln(os.Stderr, "Review full proposed file contents with 'pack get --preview' before confirming.")
			}
		}
		if preview {
			return json.NewEncoder(os.Stdout).Encode(proposal)
		}
		if !yes {
			if flagJSON || flagQuiet {
				return fmt.Errorf("installation requires --yes after reviewing 'pack get --preview'")
			}
			fmt.Fprint(os.Stderr, "Install these verified files? [y/N] ")
			var answer string
			if _, err := fmt.Fscanln(cmd.InOrStdin(), &answer); err != nil || !strings.EqualFold(answer, "y") {
				return fmt.Errorf("installation cancelled")
			}
		}
		if err := prepared.Install(root); err != nil {
			return err
		}
		reg, err := registry.Open(filepath.Join(root, ".skillex", "index.db"))
		if err != nil {
			return err
		}
		defer reg.Close()
		result, err := registry.Refresh(reg, cfg, registry.RefreshOptions{Root: root, DevMode: true})
		if err != nil {
			return err
		}
		if len(result.Errors) > 0 {
			return fmt.Errorf("pack installed; refresh reported: %s", registry.FormatErrors(result.Errors))
		}
		if flagJSON {
			return json.NewEncoder(os.Stdout).Encode(map[string]any{"pack": prepared.Lock.Entry, "installed": true})
		}
		if !flagQuiet {
			fmt.Fprintln(os.Stderr, "Installed and refreshed.")
		}
		return nil
	}}
	get.Flags().BoolVar(&yes, "yes", false, "Confirm installation after reviewing the verified preview")
	get.Flags().BoolVar(&preview, "preview", false, "Show verified files and activation rules without installing")
	list := &cobra.Command{Use: "list", Short: "List installed packs after offline signature and content verification", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		installed, err := packregistry.List(repoRoot())
		if err != nil {
			return err
		}
		type summary struct {
			Name     string `json:"name"`
			Version  string `json:"version"`
			Tier     string `json:"tier"`
			Registry string `json:"registry"`
		}
		result := []summary{}
		for _, lock := range installed {
			result = append(result, summary{lock.Entry.Name, lock.Entry.Version, lock.Entry.Tier, lock.Registry})
		}
		if flagJSON {
			return json.NewEncoder(os.Stdout).Encode(result)
		}
		for _, entry := range result {
			fmt.Fprintf(os.Stdout, "%s@%s [%s] %s\n", entry.Name, entry.Version, entry.Tier, entry.Registry)
		}
		return nil
	}}
	cmd.AddCommand(get, list, newPackRemoveCmd(), newPackSearchCmd(), newPackInfoCmd())
	return cmd
}

func newPackRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <name[@version]>",
		Short: "Remove one verified installed pack and refresh the project index",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root := repoRoot()
			selected, err := packregistry.SelectInstalled(root, args[0])
			if err != nil {
				return err
			}
			cfg, err := config.Load(root)
			if err != nil {
				return err
			}
			reg, err := registry.Open(filepath.Join(root, ".skillex", "index.db"))
			if err != nil {
				return err
			}
			defer reg.Close()
			if err := reg.RemovePack(cfg, root, selected); err != nil {
				return err
			}
			if flagJSON {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"pack": selected, "removed": true})
			}
			if !flagQuiet {
				fmt.Fprintf(cmd.OutOrStdout(), "Removed %s@%s and refreshed the index.\n", selected.Name, selected.Version)
			}
			return nil
		},
	}
}

func loadPackCatalog(ctx context.Context) (*packregistry.Manifest, string, error) {
	cfg, err := config.Load(repoRoot())
	if err != nil {
		return nil, "", err
	}
	url := packregistry.ManifestURL(cfg)
	m, _, _, err := packregistry.Fetch(ctx, url)
	return m, url, err
}

func newPackSearchCmd() *cobra.Command {
	var tier string
	var limit, offset int
	cmd := &cobra.Command{Use: "search [query]", Short: "Search the signed catalog for available packs", Args: cobra.MaximumNArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		m, _, err := loadPackCatalog(cmd.Context())
		if err != nil {
			return err
		}
		page, err := m.Search(strings.Join(args, " "), tier, limit, offset)
		if err != nil {
			return err
		}
		if flagJSON {
			return json.NewEncoder(cmd.OutOrStdout()).Encode(page)
		}
		for _, r := range page.Results {
			fmt.Fprintf(cmd.OutOrStdout(), "%s@%s [%s] %s\n", r.Name, r.Version, r.Tier, r.Description)
		}
		if len(page.Results) == 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "No matching packs on this page.")
		}
		if page.NextOffset != nil {
			fmt.Fprintf(cmd.OutOrStdout(), "More results: use --offset %d\n", *page.NextOffset)
		}
		return nil
	}}
	cmd.Flags().StringVar(&tier, "tier", "", "Filter by signed registry tier")
	cmd.Flags().IntVar(&limit, "limit", packregistry.DefaultSearchLimit, "Maximum results per page (1–50)")
	cmd.Flags().IntVar(&offset, "offset", 0, "Start at this result offset")
	return cmd
}

func newPackInfoCmd() *cobra.Command {
	return &cobra.Command{Use: "info <name>", Short: "Show signed metadata for a pack's catalogued stable versions", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		m, url, err := loadPackCatalog(cmd.Context())
		if err != nil {
			return err
		}
		info, err := m.Info(url, args[0])
		if err != nil {
			return err
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		if !flagJSON {
			enc.SetIndent("", "  ")
		}
		return enc.Encode(info)
	}}
}
