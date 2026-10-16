package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/atheory-ai/skillex/internal/config"
	"github.com/atheory-ai/skillex/internal/registry"
	"github.com/atheory-ai/skillex/internal/watch"
	mcpserver "github.com/atheory-ai/skillex/mcp"
)

func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Start the MCP server (stdio transport)",
		Long: `Start the skillex MCP server using stdio transport.

The MCP server exposes:
  - Resources: bounded skill summaries and section tables of contents
  - Tools: skillex_query for discovery and skillex_read for bounded selected content
  - When explicitly enabled: downstream capability discovery, describe, and call tools

The server watches project skill directories already in the index. Edits and new
skills under those directories update the index. A skill directory created
elsewhere is picked up after skillex refresh, on the next request.

Configure in your agent harness:

  {
    "mcpServers": {
      "skillex": {
        "command": "skillex",
        "args": ["mcp"]
      }
    }
  }`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := repoRoot()
			dbPath := filepath.Join(root, ".skillex", "index.db")

			if _, err := os.Stat(dbPath); os.IsNotExist(err) {
				return fmt.Errorf("registry not found at %s — run 'skillex refresh' first", dbPath)
			}

			reg, err := registry.Open(dbPath)
			if err != nil {
				return fmt.Errorf("opening registry: %w", err)
			}
			defer reg.Close()
			cfg, err := config.Load(root)
			if err != nil {
				return err
			}

			skillWatch := watch.New(watch.Config{
				Root:     root,
				Registry: reg,
				OnError: func(err error) {
					fmt.Fprintf(os.Stderr, "skillex: skill watch: %v\n", err)
				},
			})
			var before func()
			if err := skillWatch.Arm(); err != nil {
				fmt.Fprintf(os.Stderr, "skillex: skill watch disabled: %v\n", err)
				skillWatch.Close()
			} else {
				defer skillWatch.Close()
				before = skillWatch.BeforeRequest
			}

			return mcpserver.ServeConfigured(reg, cfg, root, Version, before)
		},
	}
}
