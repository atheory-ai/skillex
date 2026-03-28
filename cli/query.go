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

When no filters are provided, a vocabulary response is returned listing the available
topics, tags, and packages you can filter by.

When filters are provided but no skills match, a no_match response is returned with
the same vocabulary as a hint to help reformulate the query.

Examples:
  skillex query --path packages/app-a/src/auth.ts
  skillex query --topic error-handling
  skillex query --tags migration,breaking-change
  skillex query --package @acme/foo
  skillex query --path packages/app-a/** --topic auth --format content`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := repoRoot()
			dbPath := filepath.Join(root, ".skillex", "index.db")

			if _, err := os.Stat(dbPath); os.IsNotExist(err) {
				return fmt.Errorf("registry not found — run 'skillex refresh' first")
			}

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

			resp, err := eng.Execute(params)
			if err != nil {
				return fmt.Errorf("query failed: %w", err)
			}

			if flagJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(resp)
			}

			switch resp.Type {
			case query.ResponseTypeResults:
				if format == query.FormatContent {
					out := query.ContentString(resp.Results)
					fmt.Print(out)
					if !strings.HasSuffix(out, "\n") {
						fmt.Println()
					}
				} else {
					printSummary(resp.Results)
				}

			case query.ResponseTypeVocabulary:
				if !flagQuiet {
					printVocabulary(resp.Vocabulary, "")
				}

			case query.ResponseTypeNoMatch:
				if !flagQuiet {
					printNoMatch(resp)
				}
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

func printVocabulary(v *query.Vocabulary, header string) {
	if header != "" {
		fmt.Fprintln(os.Stderr, styleDim.Render(header))
	} else {
		fmt.Fprintf(os.Stderr, "%s\n", styleDim.Render(
			fmt.Sprintf("No filters provided — %d skills indexed. Use one of the following to query:", v.TotalSkills),
		))
	}

	if len(v.Topics) > 0 {
		fmt.Fprintln(os.Stderr, styleSuccess.Render("\nTopics:"))
		for _, t := range v.Topics {
			fmt.Fprintf(os.Stderr, "  %-30s (%d)\n", t.Name, t.Count)
		}
	}

	if len(v.Tags) > 0 {
		fmt.Fprintln(os.Stderr, styleSuccess.Render("\nTags:"))
		for _, t := range v.Tags {
			fmt.Fprintf(os.Stderr, "  %-30s (%d)\n", t.Name, t.Count)
		}
	}

	if len(v.Packages) > 0 {
		fmt.Fprintln(os.Stderr, styleSuccess.Render("\nPackages:"))
		for _, p := range v.Packages {
			ver := p.Version
			if ver != "" {
				ver = "@" + ver
			}
			fmt.Fprintf(os.Stderr, "  %-30s (%d skills)\n", p.Name+ver, p.Count)
		}
	}
}

func printNoMatch(resp *query.Response) {
	var parts []string
	if resp.Query != nil {
		q := resp.Query
		if len(q.Topics) > 0 {
			parts = append(parts, fmt.Sprintf("topic=%s", strings.Join(q.Topics, ",")))
		}
		if len(q.Tags) > 0 {
			parts = append(parts, fmt.Sprintf("tags=%s", strings.Join(q.Tags, ",")))
		}
		if q.Package != "" {
			parts = append(parts, fmt.Sprintf("package=%s", q.Package))
		}
		if q.Path != "" {
			parts = append(parts, fmt.Sprintf("path=%s", q.Path))
		}
	}
	header := "No skills matched"
	if len(parts) > 0 {
		header += " (" + strings.Join(parts, ", ") + ")"
	}
	header += "."
	printVocabulary(resp.Vocabulary, header)
}
