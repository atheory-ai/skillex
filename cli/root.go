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
	}

	rootCmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "Output structured JSON to stdout")
	rootCmd.PersistentFlags().BoolVarP(&flagQuiet, "quiet", "q", false, "Suppress stderr output")

	rootCmd.AddCommand(
		newInitCmd(),
		newQueryCmd(),
		newRefreshCmd(),
		newGetCmd(),
		newImportCmd(),
		newTestCmd(),
		newDoctorCmd(),
		newVersionCmd(),
		newMCPCmd(),
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
