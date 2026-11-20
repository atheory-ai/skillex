package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atheory-ai/skillex/internal/config"
	"github.com/atheory-ai/skillex/internal/query"
	"github.com/atheory-ai/skillex/internal/registry"
	"github.com/atheory-ai/skillex/internal/steering"
	"github.com/atheory-ai/skillex/internal/upgrade"
	"github.com/spf13/cobra"
)

func newReadCmd() *cobra.Command {
	var ref, section string
	var maxBytes int
	cmd := &cobra.Command{
		Use: "read --ref <ref>", Short: "Read selected skill, core guidance, or upgrade plan within a byte budget",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if ref == "" {
				return fmt.Errorf("--ref is required (select one from skillex query)")
			}
			resp, err := readRef(cmd.Context(), ref, section, maxBytes)
			if err != nil {
				return err
			}
			notices := commandUpdateNotices(cmd.Context())
			if flagJSON {
				return writeJSONWithNotices(os.Stdout, resp, notices, false)
			}
			printCoreMarker(resp.Core)
			fmt.Print(resp.Content)
			if resp.Truncated {
				fmt.Fprintf(os.Stderr, "\n[skillex: content truncated; %s]\n", resp.NextAction)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&ref, "ref", "", "Skill, core, or upgrade plan ref returned by skillex query")
	cmd.Flags().StringVar(&section, "section", "", "Optional section id returned by skillex query")
	cmd.Flags().IntVar(&maxBytes, "max-bytes", 24*1024, "Maximum content bytes to return (up to 65536)")
	return cmd
}

func readRef(ctx context.Context, ref, section string, maxBytes int) (*query.ReadResponse, error) {
	if strings.HasPrefix(ref, upgrade.Prefix) {
		return query.ReadUpgrade(ctx, ref, section, maxBytes)
	}
	root := repoRoot()
	cfg, configErr := config.Load(root)
	if configErr != nil {
		if ref == steering.Ref {
			return nil, configErr
		}
		fmt.Fprintf(os.Stderr, "skillex: core guidance unavailable: %v\n", configErr)
	}
	core, err := cfg.CoreGuidance()
	if err != nil {
		return nil, err
	}
	reg, err := registry.Open(filepath.Join(root, ".skillex", "index.db"))
	if err != nil {
		return nil, err
	}
	defer reg.Close()
	return query.New(reg).WithCore(core).Read(ref, section, maxBytes)
}

func printCoreMarker(core *steering.Marker) {
	if core != nil && !flagQuiet {
		fmt.Fprintf(os.Stderr, "Core guidance %s (revision %s)\n", core.Ref, core.Revision)
	}
}
