package registry

import "testing"

func TestDeleteSkillByPathRemovesSkillAndSearch(t *testing.T) {
	reg := newTestRegistry(t)
	id, err := reg.InsertSkill(Skill{
		Path:       ".cursor/skills/land-it/SKILL.md",
		Content:    "# land\n",
		Name:       "land-it",
		Visibility: "repo",
		SourceType: "agent",
		Topics:     []string{"shipping"},
		Scopes:     []string{"**"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.InsertTestScenario(TestScenario{
		SkillID:  id,
		Name:     "Ship",
		Prompt:   "Ship it",
		Criteria: []string{"Ships"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.InsertSkill(Skill{
		Path:       "skills/keep.md",
		Content:    "# keep\n",
		Visibility: "repo",
		SourceType: "repo",
	}); err != nil {
		t.Fatal(err)
	}

	if err := reg.DeleteSkillByPath(".cursor/skills/land-it/SKILL.md"); err != nil {
		t.Fatal(err)
	}
	got, err := reg.GetSkillByPath(".cursor/skills/land-it/SKILL.md")
	if err != nil || got != nil {
		t.Fatalf("deleted skill = %#v, %v", got, err)
	}
	var n int
	if err := reg.db.QueryRow(`SELECT COUNT(*) FROM skill_search WHERE skill_id = ?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("search rows = %d", n)
	}
	scenarios, err := reg.GetTestScenarios(id)
	if err != nil || len(scenarios) != 0 {
		t.Fatalf("scenarios = %#v, %v", scenarios, err)
	}
	paths, err := reg.PathsBySource("agent")
	if err != nil || len(paths) != 0 {
		t.Fatalf("agent paths = %v, %v", paths, err)
	}
	keep, err := reg.GetSkillByPath("skills/keep.md")
	if err != nil || keep == nil {
		t.Fatalf("kept skill = %#v, %v", keep, err)
	}
	if err := reg.DeleteSkillByPath("missing.md"); err != nil {
		t.Fatal(err)
	}
}
