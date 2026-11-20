package registry

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atheory-ai/skillex/internal/config"
	"github.com/atheory-ai/skillex/internal/linker"
	"github.com/atheory-ai/skillex/internal/packregistry"
	"github.com/atheory-ai/skillex/internal/scanner"
	"github.com/atheory-ai/skillex/internal/steering"
	"github.com/atheory-ai/skillex/internal/trust"
	"github.com/atheory-ai/skillex/internal/validator"
)

// RefreshOptions controls the refresh behavior.
type RefreshOptions struct {
	Root    string
	DevMode bool
	DryRun  bool
}

// RefreshResult summarizes what was written.
type RefreshResult struct {
	SkillsAdded       int
	TestsAdded        int
	CapabilitiesAdded int
	Errors            []error
}

// Refresh rebuilds the registry from the given configuration.
func Refresh(reg *Registry, cfg *config.Config, opts RefreshOptions) (*RefreshResult, error) {
	if opts.DryRun {
		return rebuild(nil, cfg, opts)
	}
	tx, err := reg.beginRebuild()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck // Commit closes the transaction.
	result, err := rebuild(tx, cfg, opts)
	if err != nil {
		return nil, err
	}
	return result, tx.Commit()
}

// RemovePack restores the verified installation and rolls back the index if
// the remaining project cannot be indexed completely.
func (r *Registry) RemovePack(cfg *config.Config, root string, selected packregistry.Entry) error {
	tx, err := r.beginRebuild()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // Commit closes the transaction.
	return packregistry.Remove(root, selected, func() error {
		result, err := rebuild(tx, cfg, RefreshOptions{Root: root, DevMode: true})
		if err != nil {
			return err
		}
		if len(result.Errors) > 0 {
			return fmt.Errorf("reindexing after removal: %s", FormatErrors(result.Errors))
		}
		return tx.Commit()
	})
}

// Acquire the SQLite writer before scanning so concurrent rebuilds see filesystem
// changes in order. Readers keep their previous complete index until commit.
func (r *Registry) beginRebuild() (*sql.Tx, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	if err := (indexWriter{tx}).Clear(); err != nil {
		return nil, errors.Join(fmt.Errorf("clearing registry: %w", err), tx.Rollback())
	}
	return tx, nil
}

func rebuild(tx *sql.Tx, cfg *config.Config, opts RefreshOptions) (*RefreshResult, error) {
	// Validate saved evidence before publishing a rebuilt index. Ordinary
	// refresh works offline using the signed revocation snapshot in each lock.
	if _, err := packregistry.List(opts.Root); err != nil {
		return nil, fmt.Errorf("validating installed registry packs: %w", err)
	}
	result := &RefreshResult{}
	var capabilityRecords []CapabilityRecord
	if cfg.MCPEnabled() {
		trusted, _, err := trust.LoadConfigured()
		if err != nil {
			return nil, fmt.Errorf("loading trusted MCP configuration: %w", err)
		}
		for _, source := range cfg.MCP.Catalogs {
			loadSource := source
			sourceType := "static"
			sourceRef := source.Path
			if source.Type == "trusted" {
				if _, ok := trusted.FindCatalogSource(source.Name); !ok {
					return nil, fmt.Errorf("trusted MCP catalog %q is not defined in trusted configuration", source.Name)
				}
				loadSource = config.MCPCatalog{Type: "static", Path: trustedCatalogCacheRelPath(source.Name)}
				sourceType = "registry-api"
				sourceRef = source.Name
			}
			records, err := LoadStaticCapabilityCatalog(opts.Root, loadSource, cfg.MCP.Bindings)
			if err != nil {
				return nil, err
			}
			for i := range records {
				records[i].SourceType = sourceType
				records[i].SourceRef = sourceRef
			}
			capabilityRecords = append(capabilityRecords, records...)
		}
		observedPath := filepath.Join(opts.Root, ".skillex", "mcp", "observed.json")
		if _, err := os.Stat(observedPath); err == nil {
			records, loadErr := LoadStaticCapabilityCatalog(opts.Root, config.MCPCatalog{Type: "static", Path: ".skillex/mcp/observed.json"}, cfg.MCP.Bindings)
			if loadErr != nil {
				return nil, loadErr
			}
			for i := range records {
				records[i].SourceType = "observed"
				records[i].SourceRef = "trusted-introspection"
			}
			capabilityRecords = append(capabilityRecords, records...)
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}

	// 1. Scan
	sc := scanner.New(opts.Root, cfg, opts.DevMode)
	scanResult, err := sc.Scan()
	if err != nil {
		return nil, fmt.Errorf("scan failed: %w", err)
	}
	result.Errors = append(result.Errors, scanResult.Errors...)
	if cfg.MCPEnabled() {
		capabilityRecords, err = ApplyCapabilitySuggestions(capabilityRecords, scanResult.CapabilitySuggestions)
		if err != nil {
			return nil, fmt.Errorf("applying MCP pack suggestions: %w", err)
		}
	}

	// 2. Link
	lnk := linker.New(opts.Root, cfg)
	allSkillFiles := append(scanResult.RepoSkills, scanResult.DepSkills...)
	linkedSkills := lnk.Link(scanResult)

	// Build a map of relPath -> LinkedSkill for cross-referencing test files
	skillMap := map[string]*linker.LinkedSkill{}
	for i := range linkedSkills {
		if !linkedSkills[i].IsTest {
			skillMap[linkedSkills[i].RelPath] = &linkedSkills[i]
		}
	}

	// Build a map for test files: skillRelPath -> []SkillFile (tests). Dedupe by the test
	// file's own path: a skill listed under several scope rules is scanned once per rule,
	// so the same .test.md can appear in allSkillFiles multiple times.
	testMap := map[string][]scanner.SkillFile{}
	seenTest := map[string]bool{}
	for _, sf := range allSkillFiles {
		if sf.IsTest {
			if seenTest[sf.RelPath] {
				continue
			}
			seenTest[sf.RelPath] = true
			testMap[sf.TestFor] = append(testMap[sf.TestFor], sf)
		}
	}

	if opts.DryRun {
		// Just count what would be written
		for _, ls := range linkedSkills {
			if !ls.IsTest {
				result.SkillsAdded++
			}
		}
		for _, tests := range testMap {
			for _, tf := range tests {
				parsed, _, _ := parseScannedTest(tf) //nolint:errcheck // best-effort scenario count; parse errors are surfaced by the validator command
				if parsed != nil {
					result.TestsAdded += len(parsed.Scenarios)
				}
			}
		}
		result.CapabilitiesAdded = len(capabilityRecords)
		return result, nil
	}

	reg := indexWriter{tx}
	for _, skill := range steering.Skills() {
		if _, err := reg.InsertSkill(Skill{
			Path: skill.Ref(), Name: skill.Name, Description: skill.When, Content: skill.Content,
			Topics: []string{skill.Topic}, Visibility: "public", SourceType: steering.Source,
		}); err != nil {
			return nil, err
		}
	}

	// 4. Insert skills
	skillIDMap := map[string]int64{} // relPath -> DB id
	for _, ls := range linkedSkills {
		if ls.IsTest {
			continue
		}

		dbSkill := Skill{
			Path:           ls.RelPath,
			Content:        ls.Body,
			Name:           ls.Frontmatter.Name,
			Description:    ls.Frontmatter.Description,
			PackageName:    ls.PackageName,
			PackageVersion: ls.PackageVersion,
			Visibility:     ls.Visibility,
			SourceType:     ls.SourceType,
			Topics:         ls.Frontmatter.Topics,
			Tags:           ls.Frontmatter.Tags,
			Scopes:         ls.Scopes,
		}

		id, err := reg.InsertSkill(dbSkill)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("inserting skill %s: %w", ls.RelPath, err))
			continue
		}
		skillIDMap[ls.RelPath] = id
		result.SkillsAdded++
	}

	// 5. Insert test scenarios
	for skillPath, tests := range testMap {
		skillID, ok := skillIDMap[skillPath]
		if !ok {
			// Skill not indexed (may be a test without a skill)
			continue
		}

		for _, tf := range tests {
			parsed, _, err := parseScannedTest(tf)
			if err != nil {
				result.Errors = append(result.Errors, fmt.Errorf("parsing test %s: %w", tf.AbsPath, err))
				continue
			}
			if parsed == nil {
				continue
			}

			for _, scenario := range parsed.Scenarios {
				ts := TestScenario{
					SkillID:     skillID,
					Name:        scenario.Name,
					Prompt:      scenario.Prompt,
					ExtraSkills: scenario.ExtraSkills,
					Criteria:    scenario.Criteria,
				}
				if err := reg.InsertTestScenario(ts); err != nil {
					result.Errors = append(result.Errors, fmt.Errorf("inserting test scenario: %w", err))
					continue
				}
				result.TestsAdded++
			}
		}
	}

	// 6. Insert capability metadata. Catalog loading above is metadata-only and
	// never connects to or executes a downstream MCP server.
	for _, record := range capabilityRecords {
		if _, err := tx.Exec("SAVEPOINT capability_record"); err != nil {
			return nil, err
		}
		if _, err := insertCapability(tx, record); err != nil {
			if _, rollbackErr := tx.Exec("ROLLBACK TO capability_record"); rollbackErr != nil {
				return nil, rollbackErr
			}
			result.Errors = append(result.Errors, fmt.Errorf("inserting MCP capability %s/%s: %w",
				record.Capability.Server.Identity.CanonicalName, record.Capability.Name, err))
		} else {
			result.CapabilitiesAdded++
		}
		if _, err := tx.Exec("RELEASE capability_record"); err != nil {
			return nil, err
		}
	}

	return result, nil
}

func trustedCatalogCacheRelPath(name string) string {
	return filepath.ToSlash(filepath.Join(".skillex", "mcp", "catalogs", name+".json"))
}

func parseScannedTest(file scanner.SkillFile) (*validator.TestFile, []validator.Issue, error) {
	if file.VerifiedContent != nil {
		return validator.ParseTestContent(file.AbsPath, file.VerifiedContent)
	}
	return validator.ParseTestFile(file.AbsPath)
}

// FormatErrors formats a list of errors as a readable string.
func FormatErrors(errs []error) string {
	if len(errs) == 0 {
		return ""
	}
	var sb strings.Builder
	w := bufio.NewWriter(&sb)
	for _, e := range errs {
		fmt.Fprintln(w, "  •", e)
	}
	w.Flush()
	return sb.String()
}
