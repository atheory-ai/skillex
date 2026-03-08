package registry

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/atheory-ai/skillex/internal/config"
	"github.com/atheory-ai/skillex/internal/linker"
	"github.com/atheory-ai/skillex/internal/scanner"
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
	SkillsAdded int
	TestsAdded  int
	Errors      []error
}

// Refresh rebuilds the registry from the given configuration.
func Refresh(reg *Registry, cfg *config.Config, opts RefreshOptions) (*RefreshResult, error) {
	result := &RefreshResult{}

	// 1. Scan
	sc := scanner.New(opts.Root, cfg, opts.DevMode)
	scanResult, err := sc.Scan()
	if err != nil {
		return nil, fmt.Errorf("scan failed: %w", err)
	}
	result.Errors = append(result.Errors, scanResult.Errors...)

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

	// Build a map for test files: skillRelPath -> []SkillFile (tests)
	testMap := map[string][]scanner.SkillFile{}
	for _, sf := range allSkillFiles {
		if sf.IsTest {
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
				parsed, _, _ := validator.ParseTestFile(tf.AbsPath)
				if parsed != nil {
					result.TestsAdded += len(parsed.Scenarios)
				}
			}
		}
		return result, nil
	}

	// 3. Clear and rebuild
	if err := reg.Clear(); err != nil {
		return nil, fmt.Errorf("clearing registry: %w", err)
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
			parsed, _, err := validator.ParseTestFile(tf.AbsPath)
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

	return result, nil
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
