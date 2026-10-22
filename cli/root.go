package cli

import (
	"os"

	"github.com/spf13/cobra"
)

var (
	flagJSON  bool
	flagQuiet bool
	rootCmd   *cobra.Command
)

// Execute runs the root command.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd = &cobra.Command{
		Use:   "skillex",
		Short: "Skill management for AI agents",
		Long: `Skillex manages skills — versioned, queryable documentation —
for AI agent workflows in Node.js projects.

Skills are Markdown files with YAML frontmatter that teach agents how to use
packages, follow repo conventions, and work safely in a codebase.`,
		SilenceUsage: true,
		PersistentPreRun: func(cmd *cobra.Command, _ []string) {
			// Query/read decorate their result after successful validation. MCP
			// checks on requests so initialization never waits on the network.
			for current := cmd; current != nil; current = current.Parent() {
				if current.Name() == "completion" {
					return
				}
			}
			switch cmd.Name() {
			case "query", "read", "mcp", "version", "help", "completion", "__complete", "__completeNoDesc":
				return
			}
			commandUpdateNotices(cmd.Context())
		},
	}

	rootCmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "Output structured JSON to stdout")
	rootCmd.PersistentFlags().BoolVarP(&flagQuiet, "quiet", "q", false, "Suppress stderr output")

	rootCmd.AddCommand(
		newInitCmd(),
		newQueryCmd(),
		newReadCmd(),
		newRefreshCmd(),
		newGetCmd(),
		newPackCmd(),
		newImportCmd(),
		newTestCmd(),
		newDoctorCmd(),
		newVersionCmd(),
		newMCPCmd(),
		newCapabilityCmd(),
		newAuthCmd(),
		newCatalogCmd(),
		newTelemetryCmd(),
	)
}

// repoRoot returns the working directory or panics.
func repoRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return wd
}
