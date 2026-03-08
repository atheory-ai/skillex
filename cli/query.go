package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/atheory-ai/skillex/internal/query"
	"github.com/atheory-ai/skillex/internal/registry"
)

func newQueryCmd() *cobra.Command {
	var (
		pathFlag    string
		topicFlag   string
		tagsFlag    string
		packageFlag string
		formatFlag  string
	)

	cmd := &cobra.Command{
		Use:   "query",
		Short: "Query skills from the registry",
		Long: `Query skills by path, topic, tags, or package.

All filters are intersected — only skills matching all specified criteria are returned.

Examples:
  skillex query --path packages/app-a/src/auth.ts
  skillex query --topic error-handling
  skillex query --tags migration,breaking-change
  skillex query --package @acme/foo
  skillex query --path packages/app-a/** --topic auth --format content`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := repoRoot()
			dbPath := filepath.Join(root, ".skillex", "index.db")

			reg, err := registry.Open(dbPath)
			if err != nil {
				return fmt.Errorf("opening registry: %w — run 'skillex refresh' first", err)
			}
			defer reg.Close()

			eng := query.New(reg)

			var topics []string
			if topicFlag != "" {
				for _, t := range strings.Split(topicFlag, ",") {
					t = strings.TrimSpace(t)
					if t != "" {
						topics = append(topics, t)
					}
				}
			}

			var tags []string
			if tagsFlag != "" {
				for _, t := range strings.Split(tagsFlag, ",") {
					t = strings.TrimSpace(t)
					if t != "" {
						tags = append(tags, t)
					}
				}
			}

			format := query.FormatContent
			if formatFlag == "summary" {
				format = query.FormatSummary
			}

			params := query.Params{
				Path:    pathFlag,
				Topics:  topics,
				Tags:    tags,
				Package: packageFlag,
				Format:  format,
			}

			results, err := eng.Execute(params)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			if flagJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(results)
			}

			if len(results) == 0 {
				if !flagQuiet {
					fmt.Fprintln(os.Stderr, styleDim.Render("No skills matched the query."))
				}
				return nil
			}

			if format == query.FormatContent {
				fmt.Print(query.ContentString(results))
				if !strings.HasSuffix(query.ContentString(results), "\n") {
					fmt.Println()
				}
			} else {
				printSummary(results)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&pathFlag, "path", "", "File path or glob pattern")
	cmd.Flags().StringVar(&topicFlag, "topic", "", "Comma-separated topic filters")
	cmd.Flags().StringVar(&tagsFlag, "tags", "", "Comma-separated tag filters")
	cmd.Flags().StringVar(&packageFlag, "package", "", "Package name filter")
	cmd.Flags().StringVar(&formatFlag, "format", "content", "Output format: content or summary")

	return cmd
}

func printSummary(results []query.Result) {
	for _, r := range results {
		fmt.Printf("%s\n", styleSuccess.Render(r.Path))

		var meta []string
		if r.PackageName != "" {
			meta = append(meta, fmt.Sprintf("pkg=%s@%s", r.PackageName, r.PackageVersion))
		}
		meta = append(meta, fmt.Sprintf("visibility=%s", r.Visibility))
		if len(r.Topics) > 0 {
			meta = append(meta, fmt.Sprintf("topics=[%s]", strings.Join(r.Topics, ",")))
		}
		if len(r.Tags) > 0 {
			meta = append(meta, fmt.Sprintf("tags=[%s]", strings.Join(r.Tags, ",")))
		}
		if len(meta) > 0 {
			fmt.Printf("  %s\n", styleDim.Render(strings.Join(meta, "  ")))
		}
		fmt.Println()
	}
}
