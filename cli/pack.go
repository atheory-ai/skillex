package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/atheory-ai/skillex/internal/config"
	"github.com/atheory-ai/skillex/internal/packregistry"
	"github.com/atheory-ai/skillex/internal/packs"
	"github.com/atheory-ai/skillex/internal/registry"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newPackCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "pack", Short: "Install and inspect cryptographically verified registry packs"}
	var yes, preview bool
	get := &cobra.Command{Use: "get <name>", Short: "Verify, preview, and install a registry pack", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		root := repoRoot()
		cfg, err := config.Load(root)
		if err != nil {
			return err
		}
		manifestURL := packregistry.DefaultURL
		if len(cfg.Registries) > 0 {
			manifestURL = cfg.Registries[0].URL
		}
		prepared, err := packregistry.Prepare(cmd.Context(), manifestURL, args[0])
		if err != nil {
			return err
		}
		// Validate the engine manifest against a disposable verified tree before
		// asking for consent or writing anything inside the project.
		temp, err := os.MkdirTemp("", "skillex-pack-preview-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(temp)
		if err := prepared.WriteTree(temp); err != nil {
			return err
		}
		pack, err := packs.Load(filepath.Join(temp, "pack.yaml"))
		if err != nil {
			return err
		}
		if pack.Manifest.Name != prepared.Lock.Entry.Name || pack.Manifest.Version != prepared.Lock.Entry.Version {
			return fmt.Errorf("pack identity differs from signed manifest")
		}
		var files []string
		for name := range prepared.Files {
			files = append(files, name)
		}
		sort.Strings(files)
		if !flagQuiet {
			fmt.Fprintf(os.Stderr, "Verified %s@%s [%s] from %s\n", pack.Manifest.Name, pack.Manifest.Version, prepared.Lock.Entry.Tier, prepared.Lock.Registry)
			for _, name := range files {
				fmt.Fprintf(os.Stderr, "  + %s\n", name)
			}
			activation, err := yaml.Marshal(map[string]interface{}{"skills": pack.Manifest.Skills, "detectors": pack.Manifest.Detectors, "mcp-servers": pack.Manifest.MCPServers})
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stderr, string(activation))
			if !preview {
				fmt.Fprintln(os.Stderr, "Review full proposed file contents with 'pack get --preview' before confirming.")
			}
		}
		if preview {
			return json.NewEncoder(os.Stdout).Encode(packPreview(prepared, pack, files))
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
	cmd.AddCommand(get, list)
	return cmd
}

func packPreview(prepared *packregistry.Prepared, pack *packs.Pack, files []string) map[string]interface{} {
	contents := map[string]string{}
	for name, data := range prepared.Files {
		contents[name] = string(data)
	}
	return map[string]interface{}{"pack": prepared.Lock.Entry, "files": files, "contents": contents, "skills": pack.Manifest.Skills, "detectors": pack.Manifest.Detectors, "mcp_servers": pack.Manifest.MCPServers, "installed": false}
}
