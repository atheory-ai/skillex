package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ladyhunterbear/skillex/internal/frontmatter"
)

func newImportCmd() *cobra.Command {
	var (
		visibilityFlag string
		topicsFlag     string
		destFlag       string
		batch          bool
		skipReview     bool
	)

	cmd := &cobra.Command{
		Use:   "import <filepath>",
		Short: "Import a local file as a vendored skill",
		Long: `Import a local Markdown file through the same review and conversion
pipeline as 'skillex get', but without the fetch step.

Use this for:
  - Converting existing documentation into skillex format
  - Adopting skills shared via email, Slack, or other channels
  - Migrating from Cursor rules, Windsurf rules, etc.

The file is placed in skillex/vendor/local/ (or --dest) with frontmatter and
a test stub.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filePath := args[0]
			root := repoRoot()

			var topics []string
			if topicsFlag != "" {
				for _, t := range strings.Split(topicsFlag, ",") {
					if t = strings.TrimSpace(t); t != "" {
						topics = append(topics, t)
					}
				}
			}

			dest := destFlag
			if dest == "" {
				dest = filepath.Join(root, "skillex", "vendor", "local")
			}

			if batch {
				return runImportBatch(root, filePath, dest, visibilityFlag, topics, skipReview)
			}
			return runImport(root, filePath, dest, visibilityFlag, topics, skipReview)
		},
	}

	cmd.Flags().StringVar(&visibilityFlag, "visibility", "public", "Skill visibility: public or private")
	cmd.Flags().StringVar(&topicsFlag, "topic", "", "Comma-separated topics to assign")
	cmd.Flags().StringVar(&destFlag, "dest", "", "Destination directory (default: skillex/vendor/local/)")
	cmd.Flags().BoolVar(&batch, "batch", false, "Import an entire directory of files")
	cmd.Flags().BoolVar(&skipReview, "skip-review", false, "Skip the safety review step")

	return cmd
}

func runImport(root, filePath, dest, visibility string, topics []string, skipReview bool) error {
	if !flagQuiet {
		fmt.Fprintln(os.Stderr, styleHeader.Render("  skillex import  "))
		fmt.Fprintf(os.Stderr, "  Importing %s\n", styleDim.Render(filePath))
	}

	// Read file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("reading file: %w", err)
	}

	// Review
	if !skipReview {
		issues := reviewContent(data)
		if len(issues) > 0 {
			fmt.Fprintf(os.Stderr, "\n%s Safety review flagged %d issue(s):\n",
				styleWarn.Render("!"), len(issues))
			for _, iss := range issues {
				fmt.Fprintf(os.Stderr, "  %s %s\n", styleDim.Render("•"), iss)
			}
			if !flagQuiet {
				fmt.Fprint(os.Stderr, "\nProceed anyway? [y/N] ")
				var answer string
				fmt.Scanln(&answer)
				if strings.ToLower(answer) != "y" {
					return fmt.Errorf("aborted by user")
				}
			}
		}
	}

	// Convert and write
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("creating destination directory: %w", err)
	}

	filename := filepath.Base(filePath)
	if !strings.HasSuffix(filename, ".md") {
		filename += ".md"
	}

	skillPath := filepath.Join(dest, filename)
	testPath := filepath.Join(dest, strings.TrimSuffix(filename, ".md")+".test.md")

	// Normalize content with frontmatter
	fm, body, _ := frontmatter.Parse(data)
	if len(topics) > 0 && len(fm.Topics) == 0 {
		fm.Topics = topics
	}
	if visibility != "" {
		// Store visibility hint in source field conventionally
		_ = visibility // visibility is handled by directory placement, not frontmatter
	}

	fmStr := frontmatter.FormatFrontmatter(fm)
	normalized := body
	if fmStr != "" {
		normalized = fmStr + "\n" + body
	}

	if err := os.WriteFile(skillPath, []byte(normalized), 0o644); err != nil {
		return fmt.Errorf("writing skill: %w", err)
	}

	// Test stub
	if _, err := os.Stat(testPath); os.IsNotExist(err) {
		stub := generateTestStub(filename)
		_ = os.WriteFile(testPath, []byte(stub), 0o644)
	}

	if !flagQuiet {
		fmt.Fprintf(os.Stderr, "\n%s Imported to %s\n",
			styleSuccess.Render("✓"),
			styleDim.Render(skillPath),
		)
		fmt.Fprintln(os.Stderr, "\nNext steps:")
		fmt.Fprintln(os.Stderr, "  • Review the imported skill and edit as needed")
		fmt.Fprintln(os.Stderr, "  • Run 'skillex refresh' to index the new skill")
	}

	return nil
}

func runImportBatch(root, dir, dest, visibility string, topics []string, skipReview bool) error {
	if !flagQuiet {
		fmt.Fprintf(os.Stderr, "  Batch importing from %s\n", styleDim.Render(dir))
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("reading directory: %w", err)
	}

	imported := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		filePath := filepath.Join(dir, entry.Name())
		if err := runImport(root, filePath, dest, visibility, topics, skipReview); err != nil {
			fmt.Fprintf(os.Stderr, "  %s %s: %v\n", styleError.Render("✗"), entry.Name(), err)
			continue
		}
		imported++
	}

	if !flagQuiet {
		fmt.Fprintf(os.Stderr, "\n%s Imported %d files\n", styleSuccess.Render("✓"), imported)
	}
	return nil
}
