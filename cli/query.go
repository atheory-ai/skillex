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
		searchFlag  string
		formatFlag  string
	)

	cmd := &cobra.Command{
		Use:   "query",
		Short: "Query skills from the registry",
		Long: `Query skills by path, topic, tags, package, or keyword search.

All filters are intersected — only skills matching all specified criteria are returned.

Use --search for intent-based discovery when you don't know the topic/tag taxonomy.
Each space or comma-separated term is matched independently against skill names and
descriptions, so multiple concepts can be found in one call.

When no filters are provided, a vocabulary response is returned listing the available
topics, tags, and packages you can filter by.

When filters are provided but no skills match, a no_match response is returned with
the same vocabulary as a hint to help reformulate the query.

Examples:
  skillex query --search "pagination card search"
  skillex query --search "auth" --topic security
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

			// Resolve format: explicit flag overrides default; empty means auto.
			var format query.Format
			switch formatFlag {
			case "content":
				format = query.FormatContent
			case "summary":
				format = query.FormatSummary
			default:
				format = query.FormatDefault
			}

			params := query.Params{
				Path:    pathFlag,
				Topics:  topics,
				Tags:    tags,
				Package: packageFlag,
				Search:  searchFlag,
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
				// Determine what format was actually used (mirrors Execute logic).
				effectiveFormat := format
				if effectiveFormat == query.FormatDefault {
					if searchFlag != "" {
						effectiveFormat = query.FormatSummary
					} else {
						effectiveFormat = query.FormatContent
					}
				}
				if effectiveFormat == query.FormatContent {
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
	cmd.Flags().StringVar(&searchFlag, "search", "", "Keyword search across skill names and descriptions (space/comma-separated terms)")
	cmd.Flags().StringVar(&formatFlag, "format", "", "Output format: content (default) or summary")

	return cmd
}

func printSummary(results []query.Result) {
	for _, r := range results {
		fmt.Printf("%s\n", styleSuccess.Render(r.Path))

		if r.Name != "" {
			fmt.Printf("  %s\n", r.Name)
		}
		if r.Description != "" {
			desc := r.Description
			if len(desc) > 120 {
				desc = desc[:117] + "..."
			}
			fmt.Printf("  %s\n", styleDim.Render(desc))
		}

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
		if q.Search != "" {
			parts = append(parts, fmt.Sprintf("search=%q", q.Search))
		}
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
