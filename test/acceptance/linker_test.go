package acceptance

import (
	"strings"
	"testing"

	"github.com/atheory-ai/skillex/test/helpers"
)

func TestLinker_PublicSkillsForDependencies(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	skills := queryResults(t, dir, "--path", "packages/app-a/src/auth.ts", "--format", "summary")

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

	uiSkills := queryResults(t, dir, "--package", "@test/ui", "--format", "summary")

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

	appASkills := queryResults(t, dir,
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

	skills := queryResults(t, dir, "--path", "packages/app-a/src/auth.ts", "--format", "summary")

	// All three rule layers present simultaneously
	helpers.AssertSkillPresent(t, skills, "repo.md")        // from ** rule
	helpers.AssertSkillPresent(t, skills, "package-dev.md") // from packages/*/** rule
	helpers.AssertSkillPresent(t, skills, "components.md")  // from boundary
}

func TestLinker_AllDependenciesContribute(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	skills := queryResults(t, dir, "--path", "packages/app-a/src/auth.ts", "--format", "summary")

	helpers.AssertSkillPresent(t, skills, "components.md") // from @test/ui
	helpers.AssertSkillPresent(t, skills, "api.md")        // from @test/utils
}

func TestLinker_VendorSkillsInScope(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	skills := queryResults(t, dir, "--topic", "react", "--format", "summary")

	helpers.AssertSkillPresent(t, skills, "hooks.md")
}

func TestLinker_MultiVersionBoundaryScopes(t *testing.T) {
	dir := helpers.CopyFixture(t, "multi-version-local")
	helpers.Run(t, dir, "refresh")

	appASkills := queryResults(t, dir,
		"--path", "apps/app-a/**",
		"--package", "@demo/component-library",
		"--format", "summary")

	if len(appASkills) == 0 {
		t.Fatal("expected app-a skills, got none")
	}
	for _, s := range appASkills {
		if s.Version != "1.0.0" {
			t.Errorf("app-a should only see @demo/component-library@1.0.0, got %s from %s", s.Version, s.Path)
		}
		if s.Visibility == "private" {
			t.Errorf("private v1 skill visible from app-a consumer path: %s", s.Path)
		}
	}
	helpers.AssertSkillPresent(t, appASkills, "consumer.md")

	appBSkills := queryResults(t, dir,
		"--path", "apps/app-b/**",
		"--package", "@demo/component-library",
		"--format", "summary")

	if len(appBSkills) == 0 {
		t.Fatal("expected app-b skills, got none")
	}
	for _, s := range appBSkills {
		if s.Version != "2.0.0" {
			t.Errorf("app-b should only see @demo/component-library@2.0.0, got %s from %s", s.Version, s.Path)
		}
		if s.Visibility == "private" {
			t.Errorf("private v2 skill visible from app-b consumer path: %s", s.Path)
		}
	}
	helpers.AssertSkillPresent(t, appBSkills, "consumer.md")
}

func TestLinker_MultiVersionPrivateScopes(t *testing.T) {
	dir := helpers.CopyFixture(t, "multi-version-local")
	helpers.Run(t, dir, "refresh")

	maintainerSkills := queryResults(t, dir,
		"--path", "apps/app-a/node_modules/@demo/component-library/**",
		"--package", "@demo/component-library",
		"--format", "summary")

	hasPrivate := false
	for _, s := range maintainerSkills {
		if s.Version != "1.0.0" {
			t.Errorf("app-a package path should only see v1 skills, got %s from %s", s.Version, s.Path)
		}
		if s.Visibility == "private" {
			hasPrivate = true
		}
	}
	if !hasPrivate {
		t.Fatal("expected private v1 skills when querying inside app-a's installed package path")
	}
}
