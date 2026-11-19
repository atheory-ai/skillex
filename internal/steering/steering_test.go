package steering_test

import (
	"path/filepath"
	"testing"

	"github.com/atheory-ai/skillex/internal/steering"
	"github.com/atheory-ai/skillex/internal/validator"
)

func TestBundledSkillScenarios(t *testing.T) {
	for _, skill := range steering.Skills() {
		t.Run(skill.ID, func(t *testing.T) {
			test, issues, err := validator.ParseTestFile(filepath.Join("skills", skill.ID+".test.md"))
			if err != nil || len(issues) != 0 {
				t.Fatalf("invalid bundled scenarios: %v, %v", issues, err)
			}
			if len(test.Scenarios) == 0 {
				t.Fatal("expected a scenario with a prompt and success criteria")
			}
		})
	}
}
