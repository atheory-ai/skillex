package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"

	"github.com/atheory-ai/skillex/internal/agents"
	"github.com/atheory-ai/skillex/internal/config"
	"github.com/atheory-ai/skillex/internal/registry"
)

func newRefreshCmd() *cobra.Command {
	var (
		mode   string
		check  bool
		dryRun bool
	)

	cmd := &cobra.Command{
		Use:   "refresh",
		Short: "Rebuild the skill registry",
		Long: `Rebuild the registry from skillex.yaml and installed packages.

By default, refresh includes devDependencies. Use --mode prod to include
only production dependencies and public skills.

Use --check in CI to fail if the registry is stale.
Use --dry-run to preview what would change without writing.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := repoRoot()

			cfg, err := config.Load(root)
			if err != nil {
				return err
			}

			dbPath := filepath.Join(root, ".skillex", "index.db")
			devMode := mode != "prod"

			if check {
				return runRefreshCheck(root, dbPath, cfg, devMode)
			}

			reg, err := registry.Open(dbPath)
			if err != nil {
				return fmt.Errorf("opening registry: %w", err)
			}
			defer reg.Close()

			opts := registry.RefreshOptions{
				Root:    root,
				DevMode: devMode,
				DryRun:  dryRun,
			}

			if !flagQuiet {
				fmt.Fprintln(os.Stderr, styleHeader.Render("  skillex refresh  "))
				if dryRun {
					fmt.Fprintln(os.Stderr, styleInfo.Render("dry run — no changes will be written"))
				}
			}

			result, err := registry.Refresh(reg, cfg, opts)
			if err != nil {
				return err
			}

			if len(result.Errors) > 0 && !flagQuiet {
				fmt.Fprintln(os.Stderr, styleWarn.Render("Warnings:"))
				fmt.Fprint(os.Stderr, registry.FormatErrors(result.Errors))
			}

			if flagJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(map[string]any{
					"skills_added": result.SkillsAdded,
					"tests_added":  result.TestsAdded,
					"errors":       len(result.Errors),
					"dry_run":      dryRun,
				})
			}

			if !flagQuiet {
				fmt.Fprintf(os.Stderr, "%s %d skills, %d test scenarios\n",
					styleSuccess.Render("✓"),
					result.SkillsAdded,
					result.TestsAdded,
				)
			}

			if !dryRun {
				agentsPath := filepath.Join(root, "AGENTS.md")
				section, err := agents.GenerateSection(reg)
				if err != nil {
					return fmt.Errorf("generating AGENTS.md section: %w", err)
				}
				if err := agents.UpdateFile(agentsPath, section); err != nil {
					return fmt.Errorf("updating AGENTS.md: %w", err)
				}
				if !flagQuiet {
					fmt.Fprintln(os.Stderr, styleSuccess.Render("✓")+" AGENTS.md updated")
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&mode, "mode", "dev", "Dependency mode: dev (default) or prod")
	cmd.Flags().BoolVar(&check, "check", false, "Fail if registry is stale (for CI)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview changes without writing")

	return cmd
}

func runRefreshCheck(root, dbPath string, cfg *config.Config, devMode bool) error {
	tempPath := dbPath + ".check"
	defer os.Remove(tempPath)

	tempReg, err := registry.Open(tempPath)
	if err != nil {
		return err
	}
	defer tempReg.Close()

	opts := registry.RefreshOptions{Root: root, DevMode: devMode}
	freshResult, err := registry.Refresh(tempReg, cfg, opts)
	if err != nil {
		return err
	}

	existing, err := registry.Open(dbPath)
	if err != nil {
		return fmt.Errorf("registry not found at %s — run 'skillex refresh' first", dbPath)
	}
	defer existing.Close()

	existingCount, err := existing.SkillCount()
	if err != nil {
		return err
	}

	if existingCount != freshResult.SkillsAdded {
		return fmt.Errorf("registry is stale: current has %d skills, fresh scan has %d",
			existingCount, freshResult.SkillsAdded)
	}

	if !flagQuiet {
		fmt.Fprintf(os.Stderr, "registry is up to date (%d skills)\n", existingCount)
	}
	return nil
}

// Styles used across CLI commands.
var (
	styleHeader  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6")).Padding(0, 1)
	styleSuccess = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleError   = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styleWarn    = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styleInfo    = lipgloss.NewStyle().Foreground(lipgloss.Color("4"))
	styleDim     = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)
