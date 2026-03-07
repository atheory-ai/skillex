package validator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Issue represents a validation problem.
type Issue struct {
	File    string
	Message string
	Level   string // "error" or "warning"
}

// TestFile holds a parsed test file.
type TestFile struct {
	Path      string
	SkillRef  string
	Scenarios []TestScenario
}

// TestScenario represents a single validation block in a test file.
type TestScenario struct {
	Name        string
	Prompt      string
	ExtraSkills []string
	Criteria    []string
}

// ValidateAll validates all skill and test files in the given directories.
func ValidateAll(dirs []string) ([]Issue, error) {
	var issues []Issue

	for _, dir := range dirs {
		dirIssues, err := validateDirectory(dir)
		if err != nil {
			return nil, err
		}
		issues = append(issues, dirIssues...)
	}

	return issues, nil
}

// validateDirectory validates skills and test files within a directory.
func validateDirectory(dir string) ([]Issue, error) {
	var issues []Issue

	skillFiles := map[string]bool{}
	testFiles := map[string]string{} // test path -> skill path it references

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}

		rel, _ := filepath.Rel(dir, path)

		if strings.HasSuffix(path, ".test.md") {
			skillPath := strings.TrimSuffix(path, ".test.md") + ".md"
			testFiles[path] = skillPath

			// Parse and validate the test file
			tf, parseIssues, err := ParseTestFile(path)
			if err != nil {
				issues = append(issues, Issue{File: rel, Message: fmt.Sprintf("parse error: %v", err), Level: "error"})
				return nil
			}
			issues = append(issues, parseIssues...)
			_ = tf // used implicitly via parseIssues
		} else {
			skillFiles[path] = true
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	// Check: every .test.md has a corresponding .md
	for testPath, skillPath := range testFiles {
		testRel, _ := filepath.Rel(dir, testPath)
		if !skillFiles[skillPath] {
			issues = append(issues, Issue{
				File:    testRel,
				Message: fmt.Sprintf("orphaned test file: corresponding skill %s does not exist", skillPath),
				Level:   "error",
			})
		}
	}

	// Check: every skill has a corresponding test (warning only)
	for skillPath := range skillFiles {
		testPath := strings.TrimSuffix(skillPath, ".md") + ".test.md"
		if _, exists := testFiles[testPath]; !exists {
			skillRel, _ := filepath.Rel(dir, skillPath)
			issues = append(issues, Issue{
				File:    skillRel,
				Message: "missing test file",
				Level:   "warning",
			})
		}
	}

	return issues, nil
}

// ParseTestFile parses a .test.md file and validates its structure.
func ParseTestFile(path string) (*TestFile, []Issue, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}

	rel := filepath.Base(path)
	var issues []Issue
	tf := &TestFile{Path: path}

	lines := strings.Split(string(data), "\n")
	var currentSection *TestScenario

	for i, line := range lines {
		lineNum := i + 1

		if strings.HasPrefix(line, "# Tests: ") {
			tf.SkillRef = strings.TrimPrefix(line, "# Tests: ")
			continue
		}

		if strings.HasPrefix(line, "## Validation: ") {
			if currentSection != nil {
				if err := validateScenario(currentSection, rel, &issues); err == nil {
					tf.Scenarios = append(tf.Scenarios, *currentSection)
				}
			}
			currentSection = &TestScenario{
				Name: strings.TrimPrefix(line, "## Validation: "),
			}
			continue
		}

		if currentSection == nil {
			continue
		}

		if strings.HasPrefix(line, "Prompt: ") {
			currentSection.Prompt = strings.TrimPrefix(line, "Prompt: ")
			continue
		}

		if strings.HasPrefix(line, "Skills: ") {
			skillList := strings.TrimPrefix(line, "Skills: ")
			for _, s := range strings.Split(skillList, ",") {
				s = strings.TrimSpace(s)
				if s != "" {
					currentSection.ExtraSkills = append(currentSection.ExtraSkills, s)
				}
			}
			continue
		}

		if strings.HasPrefix(line, "Success criteria:") {
			// Criteria follow as a list
			continue
		}

		if strings.HasPrefix(line, "  - ") {
			currentSection.Criteria = append(currentSection.Criteria, strings.TrimPrefix(line, "  - "))
			continue
		}

		_ = lineNum // used for future line-number error reporting
	}

	// Don't forget the last section
	if currentSection != nil {
		if err := validateScenario(currentSection, rel, &issues); err == nil {
			tf.Scenarios = append(tf.Scenarios, *currentSection)
		}
	}

	// Validate H1 present
	if tf.SkillRef == "" {
		issues = append(issues, Issue{
			File:    rel,
			Message: "missing H1 'Tests: <filename>' header",
			Level:   "error",
		})
	}

	if len(tf.Scenarios) == 0 && tf.SkillRef != "" {
		issues = append(issues, Issue{
			File:    rel,
			Message: "no validation scenarios found",
			Level:   "warning",
		})
	}

	return tf, issues, nil
}

func validateScenario(s *TestScenario, file string, issues *[]Issue) error {
	var errs []string
	if s.Prompt == "" {
		errs = append(errs, "missing Prompt:")
	}
	if len(s.Criteria) == 0 {
		errs = append(errs, "missing Success criteria:")
	}
	if len(errs) > 0 {
		for _, e := range errs {
			*issues = append(*issues, Issue{
				File:    file,
				Message: fmt.Sprintf("scenario '%s': %s", s.Name, e),
				Level:   "error",
			})
		}
		return fmt.Errorf("invalid scenario")
	}
	return nil
}
