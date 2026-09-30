package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/atheory-ai/skillex/internal/agents"
	"github.com/atheory-ai/skillex/internal/config"
	"github.com/atheory-ai/skillex/internal/registry"
)

func newInitCmd() *cobra.Command {
	var (
		yes          bool
		pkg          bool
		harness      string
		invocation   string
		useYAML      bool
		noMCP        bool
		overwriteMCP bool
	)

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a repository for skillex",
		Long: `Initialize a repository (or package) for skillex.

For repos:
  Creates skillex.json by default (or skillex.yaml with --yaml), skills/,
  AGENTS.md, .skillex/, and optionally
  configures MCP integration for the specified agent harness.

For packages (--package):
  Creates skillex/public/ and skillex/private/ directories and adds
  "skillex": true to package.json.

Flags:
  --harness  Configure MCP for a specific harness (cursor, claude-code, windsurf)
  --no-mcp   Skip the interactive MCP configuration offer
  --overwrite-mcp  Update an existing Skillex server command (requires --harness)
  --package  Initialize this directory as a skill-exporting package
  --yaml     Generate skillex.yaml instead of the default skillex.json
  --yes      Accept all defaults without prompting`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := repoRoot()
			if invocation != "" {
				if _, err := parseInvocation(invocation); err != nil {
					return err
				}
			}

			if noMCP && harness != "" {
				return fmt.Errorf("--no-mcp cannot be combined with --harness")
			}
			if overwriteMCP && harness == "" {
				return fmt.Errorf("--overwrite-mcp requires --harness")
			}
			if harness != "" {
				if _, err := harnessConfigPath(root, harness); err != nil {
					return err
				}
			}
			if pkg {
				return initPackage(root, yes)
			}
			return initRepo(root, yes, harness, useYAML, invocation, noMCP, overwriteMCP)
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Accept all defaults without prompting")
	cmd.Flags().BoolVar(&pkg, "package", false, "Initialize as a skill-exporting package")
	cmd.Flags().StringVar(&harness, "harness", "", "Configure MCP for harness: cursor, claude-code, windsurf")
	cmd.Flags().StringVar(&invocation, "invocation", "", "Invocation: global, npm, pnpm, yarn-classic, yarn-berry, source")
	cmd.Flags().BoolVar(&noMCP, "no-mcp", false, "Skip the interactive MCP configuration offer")
	cmd.Flags().BoolVar(&overwriteMCP, "overwrite-mcp", false, "Update command and args of an existing Skillex MCP entry (requires --harness)")
	cmd.Flags().BoolVar(&useYAML, "yaml", false, "Generate skillex.yaml instead of the default skillex.json")

	return cmd
}

// initRepo sets up a repository root for skillex.
func initRepo(root string, yes bool, harness string, useYAML bool, invocation string, noMCP, overwriteMCP bool) error {
	input := bufio.NewReader(os.Stdin)
	if !flagQuiet {
		fmt.Fprintln(os.Stderr, styleHeader.Render("  skillex init  "))
	}

	configPath := config.JSONFilename
	configFormat := config.FormatJSON
	if useYAML {
		configPath = config.YAMLFilename
		configFormat = config.FormatYAML
	}

	if invocation == "" {
		if _, err := config.Load(root); err != nil {
			invocation = detectInvocation(root)
			if !yes && terminalInput() {
				invocation, err = chooseInvocation(input, os.Stderr, invocation)
				if err != nil {
					return err
				}
			}
		}
	}

	if harness == "" && !noMCP && !yes && terminalInput() {
		var err error
		harness, err = chooseHarness(input, os.Stderr, detectHarness(root))
		if err != nil {
			return err
		}
	}

	steps := []struct {
		desc string
		fn   func() error
	}{
		{fmt.Sprintf("Creating %s", configPath), func() error { return createSkilexConfig(root, yes, configFormat) }},
		{"Selecting project invocation", func() error { return setupInvocation(root, yes, invocation, input) }},
		{"Creating skills/ directory", func() error { return createSkillsDir(root) }},
		{"Creating .skillex/ directory", func() error { return os.MkdirAll(filepath.Join(root, ".skillex"), 0o755) }},
		{"Writing AGENTS.md", func() error { return createAgentsMD(root) }},
	}

	if harness != "" {
		steps = append(steps, struct {
			desc string
			fn   func() error
		}{
			fmt.Sprintf("Configuring MCP for %s", harness),
			func() error { return configureMCP(root, harness, overwriteMCP) },
		})
	}

	for _, step := range steps {
		if !flagQuiet {
			fmt.Fprintf(os.Stderr, "  %s %s\n", styleDim.Render("→"), step.desc)
		}
		if err := step.fn(); err != nil {
			fmt.Fprintf(os.Stderr, "  %s %s: %v\n", styleError.Render("✗"), step.desc, err)
			return err
		}
		if !flagQuiet {
			fmt.Fprintf(os.Stderr, "  %s %s\n", styleSuccess.Render("✓"), step.desc)
		}
	}

	// Run initial refresh
	if !flagQuiet {
		fmt.Fprintln(os.Stderr, "\nRunning initial refresh...")
	}

	cfg, err := config.Load(root)
	if err != nil {
		return err
	}

	dbPath := filepath.Join(root, ".skillex", "index.db")
	reg, err := registry.Open(dbPath)
	if err != nil {
		return err
	}
	defer reg.Close()

	result, err := registry.Refresh(reg, cfg, registry.RefreshOptions{Root: root, DevMode: true})
	if err != nil {
		return err
	}

	section := agents.GenerateSectionWithCommand(cfg.Install.CLI())
	agentsPath := filepath.Join(root, "AGENTS.md")
	if err := agents.UpdateFile(agentsPath, section); err != nil {
		return err
	}
	if _, err := agents.UpdateBridgeFiles(root); err != nil {
		return err
	}

	if !flagQuiet {
		fmt.Fprintf(os.Stderr, "\n%s Repository initialized! %d skills indexed.\n",
			styleSuccess.Render("✓"), result.SkillsAdded)
		fmt.Fprintln(os.Stderr, "\nNext steps:")
		fmt.Fprintln(os.Stderr, "  • Edit skills/repo.md to add your first repo-level skill")
		fmt.Fprintf(os.Stderr, "  • Run '%s refresh' after making changes\n", cfg.Install.CLI())
		fmt.Fprintf(os.Stderr, "  • Run '%s doctor' to check for issues\n", cfg.Install.CLI())
	}

	return nil
}

// initPackage sets up a package for skillex skill exports.
func initPackage(root string, yes bool) error {
	if !flagQuiet {
		fmt.Fprintln(os.Stderr, styleHeader.Render("  skillex init --package  "))
	}

	// Create skillex/public/ and skillex/private/
	for _, dir := range []string{"skillex/public", "skillex/private"} {
		path := filepath.Join(root, dir)
		if err := os.MkdirAll(path, 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", dir, err)
		}
		if !flagQuiet {
			fmt.Fprintf(os.Stderr, "  %s Created %s/\n", styleSuccess.Render("✓"), dir)
		}
	}

	// Create starter skill files
	publicSkill := filepath.Join(root, "skillex/public/consumer.md")
	if _, err := os.Stat(publicSkill); os.IsNotExist(err) {
		content := "---\nname: Consumer Guide\ndescription: How to install, configure, and use this package as a dependency.\ntopics: []\ntags: []\n---\n\n# Consumer Guide\n\nDocument how consumers should use this package.\n"
		if err := os.WriteFile(publicSkill, []byte(content), 0o644); err != nil { //nolint:gosec // G306: skill template the user will edit
			return err
		}
	}

	privateSkill := filepath.Join(root, "skillex/private/dev-workflow.md")
	if _, err := os.Stat(privateSkill); os.IsNotExist(err) {
		content := "---\nname: Development Workflow\ndescription: How contributors set up, build, test, and submit changes to this package.\ntopics: []\ntags: []\n---\n\n# Development Workflow\n\nDocument how contributors work on this package.\n"
		if err := os.WriteFile(privateSkill, []byte(content), 0o644); err != nil { //nolint:gosec // G306: skill template the user will edit
			return err
		}
	}

	// Update package.json
	pkgJSONPath := filepath.Join(root, "package.json")
	if err := addSkilexToPackageJSON(pkgJSONPath); err != nil {
		if !flagQuiet {
			fmt.Fprintf(os.Stderr, "  %s Could not update package.json: %v\n", styleWarn.Render("!"), err)
			fmt.Fprintln(os.Stderr, `    Add "skillex": true manually to enable skill exports.`)
		}
	} else {
		if !flagQuiet {
			fmt.Fprintf(os.Stderr, "  %s Added \"skillex\": true to package.json\n", styleSuccess.Render("✓"))
		}
	}

	if !flagQuiet {
		fmt.Fprintf(os.Stderr, "\n%s Package initialized for skillex exports.\n", styleSuccess.Render("✓"))
		fmt.Fprintln(os.Stderr, "\nNext steps:")
		fmt.Fprintln(os.Stderr, "  • Edit skillex/public/consumer.md")
		fmt.Fprintln(os.Stderr, "  • Edit skillex/private/dev-workflow.md")
		fmt.Fprintln(os.Stderr, "  • Run 'skillex refresh' in the repo root to index changes")
	}

	return nil
}

func createSkilexConfig(root string, yes bool, format config.Format) error {
	filename := config.JSONFilename
	if format == config.FormatYAML {
		filename = config.YAMLFilename
	}
	path := filepath.Join(root, filename)

	if _, err := os.Stat(path); err == nil {
		if !yes {
			fmt.Fprintf(os.Stderr, "  %s %s already exists, skipping\n", styleDim.Render("→"), filename)
		}
		return nil
	}

	if otherPath, _, err := config.ResolvePath(root); err == nil {
		if !yes {
			fmt.Fprintf(os.Stderr, "  %s %s already exists, skipping\n", styleDim.Render("→"), filepath.Base(otherPath))
		}
		return nil
	}

	cfg := config.DefaultConfig()
	install, err := parseInvocation(detectInvocation(root))
	if err != nil {
		return err
	}
	cfg.Install = install
	data, err := config.Marshal(cfg, format)
	if err != nil {
		return err
	}

	if format == config.FormatYAML {
		header := "# Skillex configuration\n# See https://github.com/atheory-ai/skillex for documentation\n\n"
		data = append([]byte(header), data...)
	}

	return os.WriteFile(path, data, 0o644) //nolint:gosec // G306: skillex.yaml is user-edited config
}

func createSkillsDir(root string) error {
	dir := filepath.Join(root, "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	repoSkill := filepath.Join(dir, "repo.md")
	if _, err := os.Stat(repoSkill); os.IsNotExist(err) {
		content := `---
name: Repository Conventions
description: Coding standards, branch naming, commit message format, and PR process for this repository.
topics: [repo-conventions]
tags: [getting-started]
---

# Repository Conventions

Document your repository-wide conventions here. This skill is loaded for all
paths in the repository.

## Examples

- Coding style and formatting
- Branch naming conventions
- Commit message format
- PR process
`
		return os.WriteFile(repoSkill, []byte(content), 0o644) //nolint:gosec // G306: skill template the user will edit
	}
	return nil
}

func createAgentsMD(root string) error {
	path := filepath.Join(root, "AGENTS.md")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return os.WriteFile(path, []byte(agents.DefaultContent()), 0o644) //nolint:gosec // G306: AGENTS.md is user-edited project doc
	}
	return nil
}

func addSkilexToPackageJSON(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("package.json not found")
		}
		return err
	}

	content := string(data)

	// Check if skillex field already exists
	if contains(content, `"skillex"`) {
		return nil
	}

	// Simple insertion before the closing brace
	// This is intentionally simple; a full JSON parser would be more robust
	lastBrace := findLastBrace(content)
	if lastBrace == -1 {
		return fmt.Errorf("could not find closing brace in package.json")
	}

	// Check if we need a comma
	prefix := content[:lastBrace]
	needsComma := len(trimRight(prefix)) > 1 // more than just {

	insertion := "\n  \"skillex\": true"
	if needsComma {
		insertion = "," + insertion
	}

	updated := content[:lastBrace] + insertion + "\n" + content[lastBrace:]
	return os.WriteFile(path, []byte(updated), 0o644) //nolint:gosec // G306: project's own package.json
}

func findLastBrace(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '}' {
			return i
		}
	}
	return -1
}

func trimRight(s string) string {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] != ' ' && s[i] != '\t' && s[i] != '\n' && s[i] != '\r' {
			return s[:i+1]
		}
	}
	return ""
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
