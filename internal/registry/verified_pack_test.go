package registry

import (
	"path/filepath"
	"testing"

	"github.com/atheory-ai/skillex/internal/scanner"
)

func TestVerifiedPackScenariosDoNotReopenSource(t *testing.T) {
	file := scanner.SkillFile{AbsPath: filepath.Join(t.TempDir(), "missing.test.md"), VerifiedContent: []byte("# Tests: usage.md\n\n## Validation: authentic\n\nPrompt: Original signed prompt\nSuccess criteria:\n  - Original criterion\n")}
	parsed, _, err := parseScannedTest(file)
	if err != nil || parsed == nil || len(parsed.Scenarios) != 1 || parsed.Scenarios[0].Prompt != "Original signed prompt" {
		t.Fatalf("verified tests reopened mutable source: %v; %#v", err, parsed)
	}
}
