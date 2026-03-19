package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags.
var Version = "dev"

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show the skillex version",
		RunE: func(cmd *cobra.Command, args []string) error {
			if flagJSON {
				fmt.Printf(`{"version":%q}`+"\n", Version)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "skillex %s\n", Version)
			}
			return nil
		},
	}
}
