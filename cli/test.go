package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/ladyhunterbear/skillex/internal/config"
	"github.com/ladyhunterbear/skillex/internal/validator"
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

			// Collect directories to validate
			dirs := collectSkillDirs(root, cfg, scope)

			if len(dirs) == 0 {
				if !flagQuiet {
					fmt.Fprintln(os.Stderr, styleDim.Render("No skill directories found to validate."))
				}
				return nil
			}

			issues, err := validator.ValidateAll(dirs)
			if err != nil {
				return fmt.Errorf("validation error: %w", err)
			}

			if flagJSON {
				type issueJSON struct {
					File    string `json:"file"`
					Level   string `json:"level"`
					Message string `json:"message"`
				}
				var out []issueJSON
				for _, iss := range issues {
					out = append(out, issueJSON{File: iss.File, Level: iss.Level, Message: iss.Message})
				}
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(out)
			}

			errCount := 0
			warnCount := 0

			if !flagQuiet {
				fmt.Fprintln(os.Stderr, styleHeader.Render("  skillex test validate  "))
			}

			for _, iss := range issues {
				switch iss.Level {
				case "error":
					errCount++
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

// collectSkillDirs gathers all directories that contain skill files.
func collectSkillDirs(root string, cfg *config.Config, scope string) []string {
	seen := map[string]bool{}
	var dirs []string

	// Repo-level skill directory
	skillsDir := filepath.Join(root, "skills")
	if info, err := os.Stat(skillsDir); err == nil && info.IsDir() {
		if !seen[skillsDir] {
			seen[skillsDir] = true
			dirs = append(dirs, skillsDir)
		}
	}

	// Skill files from rules
	for _, rule := range cfg.Rules {
		for _, skillPath := range rule.Skills {
			dir := filepath.Dir(filepath.Join(root, skillPath))
			if !seen[dir] {
				if info, err := os.Stat(dir); err == nil && info.IsDir() {
					seen[dir] = true
					dirs = append(dirs, dir)
				}
			}
		}
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
