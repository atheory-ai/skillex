package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/atheory-ai/skillex/internal/config"
	"github.com/atheory-ai/skillex/internal/scanner"
	"github.com/atheory-ai/skillex/internal/validator"
)

func newTestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test",
		Short: "Test and validate skills",
	}

	cmd.AddCommand(newTestValidateCmd())
	return cmd
}

func newTestValidateCmd() *cobra.Command {
	var (
		check bool
		scope string
	)

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate structural integrity of skill test files",
		Long: `Validate that all test files are well-formed and paired with skill files.

Checks:
  - Every .md skill has a corresponding .test.md
  - Every .test.md has a corresponding .md skill (no orphans)
  - Test files parse correctly (H1 header, Validation sections, Prompt, Success criteria)
  - Cross-referenced Skills: entries point to files that exist

Use --check to exit with a non-zero code on any error (for CI).`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := repoRoot()

			cfg, err := config.Load(root)
			if err != nil {
				return err
			}

			dirs := collectSkillDirs(root, cfg, scope)
			if len(dirs) == 0 && len(cfg.Tasks) == 0 {
				if !flagQuiet {
					fmt.Fprintln(os.Stderr, styleDim.Render("No skill directories found to validate."))
				}
				return nil
			}
			issues, err := validateProjectSkills(root, cfg, dirs)
			if err != nil {
				return fmt.Errorf("validation error: %w", err)
			}
			errCount := 0
			for _, issue := range issues {
				if issue.Level == "error" {
					errCount++
				}
			}

			if flagJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				if err := enc.Encode(issues); err != nil {
					return err
				}
			} else {
				warnCount := 0

				if !flagQuiet {
					fmt.Fprintln(os.Stderr, styleHeader.Render("  skillex test validate  "))
				}

				for _, iss := range issues {
					switch iss.Level {
					case "error":
						if !flagQuiet {
							fmt.Fprintf(os.Stderr, "  %s %s: %s\n",
								styleError.Render("✗"), iss.File, iss.Message)
						}
					case "warning":
						warnCount++
						if !flagQuiet {
							fmt.Fprintf(os.Stderr, "  %s %s: %s\n",
								styleWarn.Render("!"), iss.File, iss.Message)
						}
					}
				}

				if !flagQuiet {
					fmt.Fprintf(os.Stderr, "\n%d error(s), %d warning(s)\n", errCount, warnCount)
				}
			}
			if check && errCount > 0 {
				return fmt.Errorf("%d validation error(s) found", errCount)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&check, "check", false, "Exit with non-zero code on errors (for CI)")
	cmd.Flags().StringVar(&scope, "scope", "", "Validate only files relevant to this scope glob")

	return cmd
}

func validateProjectSkills(root string, cfg *config.Config, dirs []string) ([]validator.Issue, error) {
	files, err := scanner.ReadTaskFiles(root, cfg.Tasks)
	if err != nil {
		return nil, err
	}
	return validator.ValidateAll(root, dirs, files)
}

// collectSkillDirs gathers all directories that contain skill files.
func collectSkillDirs(root string, cfg *config.Config, scope string) []string {
	seen := map[string]bool{}
	var dirs []string

	add := func(dir string) {
		if !seen[dir] {
			if info, err := os.Stat(dir); err == nil && info.IsDir() {
				seen[dir] = true
				dirs = append(dirs, dir)
			}
		}
	}

	// Repo-level skill directory
	add(filepath.Join(root, "skills"))

	// Skill files from rules
	for _, rule := range cfg.Rules {
		for _, skillPath := range rule.Skills {
			add(filepath.Dir(filepath.Join(root, skillPath)))
		}
	}

	// Package-level skillex/public and skillex/private directories
	for _, dir := range collectSkilexPackageDirs(root) {
		add(dir)
	}

	return dirs
}

// collectSkilexPackageDirs finds all skillex/public and skillex/private directories.
func collectSkilexPackageDirs(root string) []string {
	var dirs []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if d.Name() == "node_modules" || d.Name() == ".skillex" || d.Name() == ".git" {
			return filepath.SkipDir
		}
		base := filepath.Base(path)
		parent := filepath.Base(filepath.Dir(path))
		if (base == "public" || base == "private") && parent == "skillex" {
			dirs = append(dirs, path)
		}
		return nil
	})
	_ = err
	return dirs
}
