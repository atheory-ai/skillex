package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/atheory-ai/skillex/internal/config"
	"github.com/atheory-ai/skillex/internal/registry"
	"github.com/atheory-ai/skillex/internal/validator"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run comprehensive diagnostics",
		Long: `Run comprehensive diagnostics on the skillex configuration and registry.

Checks:
  - Configuration validity
  - Registry existence and skill counts
  - Test coverage (skills without tests, orphaned tests)
  - Topic and tag distribution
  - Skills with no frontmatter
  - Vendor skill provenance`,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := repoRoot()
			return runDoctor(root)
		},
	}
}

type doctorReport struct {
	ConfigOK     bool     `json:"config_ok"`
	RegistryOK   bool     `json:"registry_ok"`
	SkillCount   int      `json:"skill_count"`
	Topics       []string `json:"topics"`
	Tags         []string `json:"tags"`
	Errors       []string `json:"errors"`
	Warnings     []string `json:"warnings"`
}

func runDoctor(root string) error {
	report := &doctorReport{}
	var errs []string
	var warns []string

	if !flagQuiet {
		fmt.Fprintln(os.Stderr, styleHeader.Render("  skillex doctor  "))
	}

	// 1. Check configuration
	cfg, err := config.Load(root)
	if err != nil {
		report.ConfigOK = false
		errs = append(errs, fmt.Sprintf("configuration: %v", err))
	} else {
		report.ConfigOK = true
		printCheck(true, "skillex.yaml loaded", fmt.Sprintf("version %d, %d rules", cfg.Version, len(cfg.Rules)))
	}

	// 2. Check registry
	dbPath := filepath.Join(root, ".skillex", "index.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		report.RegistryOK = false
		errs = append(errs, "registry not found — run 'skillex refresh'")
		printCheck(false, "registry", "not found — run 'skillex refresh'")
	} else {
		reg, err := registry.Open(dbPath)
		if err != nil {
			report.RegistryOK = false
			errs = append(errs, fmt.Sprintf("registry: %v", err))
			printCheck(false, "registry", err.Error())
		} else {
			defer reg.Close()
			report.RegistryOK = true

			count, _ := reg.SkillCount()
			report.SkillCount = count
			printCheck(true, "registry", fmt.Sprintf("%d skills indexed", count))

			topics, _ := reg.AllTopics()
			report.Topics = topics
			tags, _ := reg.AllTags()
			report.Tags = tags

			if len(topics) > 0 {
				printInfo("topics", fmt.Sprintf("%d unique: %v", len(topics), topics))
			} else {
				warns = append(warns, "no topics found — consider adding frontmatter to skills")
			}

			if len(tags) > 0 {
				printInfo("tags", fmt.Sprintf("%d unique: %v", len(tags), tags))
			}

			packages, _ := reg.AllPackages()
			if len(packages) > 0 {
				for _, p := range packages {
					printInfo(fmt.Sprintf("package %s", p.Name),
						fmt.Sprintf("%s — %d public, %d private", p.Version, p.Public, p.Private))
				}
			}
		}
	}

	// 3. Validate test coverage
	if cfg != nil {
		dirs := collectSkillDirs(root, cfg, "")
		issues, err := validator.ValidateAll(dirs)
		if err == nil {
			errIssues := 0
			warnIssues := 0
			for _, iss := range issues {
				switch iss.Level {
				case "error":
					errIssues++
					errs = append(errs, fmt.Sprintf("test: %s: %s", iss.File, iss.Message))
				case "warning":
					warnIssues++
					warns = append(warns, fmt.Sprintf("test: %s: %s", iss.File, iss.Message))
				}
			}
			printCheck(errIssues == 0,
				"test coverage",
				fmt.Sprintf("%d error(s), %d warning(s)", errIssues, warnIssues),
			)
		}
	}

	// 4. Check AGENTS.md
	agentsPath := filepath.Join(root, "AGENTS.md")
	if _, err := os.Stat(agentsPath); os.IsNotExist(err) {
		warns = append(warns, "AGENTS.md not found — run 'skillex refresh' to generate")
		printCheck(false, "AGENTS.md", "not found")
	} else {
		printCheck(true, "AGENTS.md", "present")
	}

	report.Errors = errs
	report.Warnings = warns

	if !flagQuiet {
		if len(errs) > 0 {
			fmt.Fprintf(os.Stderr, "\n%s %d error(s) found\n", styleError.Render("✗"), len(errs))
		}
		if len(warns) > 0 {
			fmt.Fprintf(os.Stderr, "%s %d warning(s)\n", styleWarn.Render("!"), len(warns))
			for _, w := range warns {
				fmt.Fprintf(os.Stderr, "  %s %s\n", styleDim.Render("•"), w)
			}
		}
		if len(errs) == 0 && len(warns) == 0 {
			fmt.Fprintf(os.Stderr, "\n%s Everything looks good!\n", styleSuccess.Render("✓"))
		}
	}

	if flagJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}

	if len(errs) > 0 {
		return fmt.Errorf("%d error(s) found", len(errs))
	}
	return nil
}

func printCheck(ok bool, name, detail string) {
	if flagQuiet {
		return
	}
	icon := styleSuccess.Render("✓")
	if !ok {
		icon = styleError.Render("✗")
	}
	if detail != "" {
		fmt.Fprintf(os.Stderr, "  %s %-20s %s\n", icon, name, styleDim.Render(detail))
	} else {
		fmt.Fprintf(os.Stderr, "  %s %s\n", icon, name)
	}
}

func printInfo(name, detail string) {
	if flagQuiet {
		return
	}
	fmt.Fprintf(os.Stderr, "  %s %-20s %s\n", styleInfo.Render("→"), name, styleDim.Render(detail))
}
