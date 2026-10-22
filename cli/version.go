package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/atheory-ai/skillex/internal/updatecheck"
)

// Version is set at build time via -ldflags.
var Version = "dev"

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show the skillex version",
		RunE: func(cmd *cobra.Command, args []string) error {
			version := updatecheck.Version(Version)
			if flagJSON {
				fmt.Printf(`{"version":%q}`+"\n", version)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "skillex %s\n", version)
			}
			return nil
		},
	}
}
