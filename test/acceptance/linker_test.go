package acceptance

import (
	"strings"
	"testing"

	"github.com/atheory-ai/skillex/test/helpers"
)

func TestLinker_PublicSkillsForDependencies(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	var skills []helpers.SkillSummary
	helpers.RunJSON(t, dir, &skills, "query", "--path", "packages/app-a/src/auth.ts", "--format", "summary")

	helpers.AssertSkillPresent(t, skills, "components.md")
	helpers.AssertSkillPresent(t, skills, "migrations.md")
	helpers.AssertSkillPresent(t, skills, "api.md")

	for _, s := range skills {
		if strings.Contains(s.Path, "skillex/private/") {
			t.Errorf("private skill visible to app-a: %s", s.Path)
		}
	}
}

func TestLinker_PrivateSkillsForSourceTree(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	var uiSkills []helpers.SkillSummary
	helpers.RunJSON(t, dir, &uiSkills, "query", "--package", "@test/ui", "--format", "summary")

	// All @test/ui skills — find private ones
	hasPrivate := false
	for _, s := range uiSkills {
		if s.Visibility == "private" {
			hasPrivate = true
		}
	}
	if !hasPrivate {
		t.Error("expected private @test/ui skills to exist in registry")
	}
}

func TestLinker_PublicPrivateExclusion(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	var appASkills []helpers.SkillSummary
	helpers.RunJSON(t, dir, &appASkills, "query",
		"--path", "packages/app-a/src/auth.ts",
		"--package", "@test/ui",
		"--format", "summary")

	for _, s := range appASkills {
		if s.Visibility == "private" {
			t.Errorf("private @test/ui skill visible from app-a: %s", s.Path)
		}
	}

	// app-a should see only public @test/ui skills
	helpers.AssertSkillPresent(t, appASkills, "components.md")
}

func TestLinker_AdditiveRuleAccumulation(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	var skills []helpers.SkillSummary
	helpers.RunJSON(t, dir, &skills, "query", "--path", "packages/app-a/src/auth.ts", "--format", "summary")

	// All three rule layers present simultaneously
	helpers.AssertSkillPresent(t, skills, "repo.md")        // from ** rule
	helpers.AssertSkillPresent(t, skills, "package-dev.md") // from packages/*/** rule
	helpers.AssertSkillPresent(t, skills, "components.md")  // from boundary
}

func TestLinker_AllDependenciesContribute(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	var skills []helpers.SkillSummary
	helpers.RunJSON(t, dir, &skills, "query", "--path", "packages/app-a/src/auth.ts", "--format", "summary")

	helpers.AssertSkillPresent(t, skills, "components.md") // from @test/ui
	helpers.AssertSkillPresent(t, skills, "api.md")        // from @test/utils
}

func TestLinker_VendorSkillsInScope(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	var skills []helpers.SkillSummary
	helpers.RunJSON(t, dir, &skills, "query", "--topic", "react", "--format", "summary")

	helpers.AssertSkillPresent(t, skills, "hooks.md")
}
