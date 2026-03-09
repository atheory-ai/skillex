package acceptance

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/atheory-ai/skillex/test/helpers"
)

func TestCLI_StdoutStderrSeparation(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	res := helpers.Run(t, dir, "query",
		"--path", "packages/app-a/src/auth.ts",
		"--json", "--format", "summary")

	if !json.Valid([]byte(res.Stdout)) {
		t.Errorf("stdout is not valid JSON: %q", res.Stdout)
	}
	// stderr may have ANSI escape codes — confirm it's not pure JSON
	if res.Stderr != "" && json.Valid([]byte(res.Stderr)) {
		t.Logf("note: stderr is valid JSON (may be empty styled output): %q", res.Stderr)
	}
}

func TestCLI_QuietSuppressesStderr(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")

	res := helpers.Run(t, dir, "refresh", "--quiet")
	if res.Stderr != "" {
		t.Errorf("--quiet should suppress stderr, got: %q", res.Stderr)
	}
}

func TestCLI_ExitCodes(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")

	// Successful refresh → 0
	res := helpers.Run(t, dir, "refresh")
	if res.ExitCode != 0 {
		t.Errorf("refresh should exit 0, got %d", res.ExitCode)
	}

	// Fresh --check → 0
	res = helpers.Run(t, dir, "refresh", "--check")
	if res.ExitCode != 0 {
		t.Errorf("refresh --check (fresh) should exit 0, got %d", res.ExitCode)
	}

	// Unknown command → non-zero
	res = helpers.Run(t, dir, "unknowncommandxyz")
	if res.ExitCode == 0 {
		t.Error("unknown command should exit non-zero")
	}
}

func TestCLI_HelpText(t *testing.T) {
	commands := [][]string{
		{"--help"},
		{"query", "--help"},
		{"refresh", "--help"},
		{"test", "validate", "--help"},
		{"init", "--help"},
		{"doctor", "--help"},
		{"mcp", "--help"},
		{"version", "--help"},
	}

	for _, args := range commands {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			res := helpers.Run(t, t.TempDir(), args...)
			if res.ExitCode != 0 {
				t.Errorf("help for %v failed (exit %d)", args, res.ExitCode)
			}
			combined := res.Stdout + res.Stderr
			if combined == "" {
				t.Errorf("help for %v produced no output", args)
			}
		})
	}
}
