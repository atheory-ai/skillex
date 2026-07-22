package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/atheory-ai/skillex/internal/query"
	"github.com/atheory-ai/skillex/internal/registry"
	"github.com/spf13/cobra"
)

func newReadCmd() *cobra.Command {
	var ref, section string
	var maxBytes int
	cmd := &cobra.Command{
		Use: "read --ref <skill-ref>", Short: "Read one selected skill or section within a byte budget",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if ref == "" {
				return fmt.Errorf("--ref is required (select one from skillex query)")
			}
			reg, err := registry.Open(filepath.Join(repoRoot(), ".skillex", "index.db"))
			if err != nil {
				return err
			}
			defer reg.Close()
			resp, err := query.New(reg).Read(ref, section, maxBytes)
			if err != nil {
				return err
			}
			if flagJSON {
				return json.NewEncoder(os.Stdout).Encode(resp)
			}
			fmt.Print(resp.Content)
			if resp.Truncated {
				fmt.Fprintf(os.Stderr, "\n[skillex: content truncated; %s]\n", resp.NextAction)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&ref, "ref", "", "Skill ref returned by skillex query")
	cmd.Flags().StringVar(&section, "section", "", "Optional section id returned by skillex query")
	cmd.Flags().IntVar(&maxBytes, "max-bytes", 24*1024, "Maximum content bytes to return (up to 65536)")
	return cmd
}
