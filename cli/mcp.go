package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	mcpserver "github.com/atheory-ai/skillex/mcp"
	"github.com/atheory-ai/skillex/internal/registry"
)

func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Start the MCP server (stdio transport)",
		Long: `Start the skillex MCP server using stdio transport.

The MCP server exposes:
  - Resources: each skill as a discoverable MCP resource
  - Tool: skillex_query for structured skill queries

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

			return mcpserver.Serve(reg, Version)
		},
	}
}
