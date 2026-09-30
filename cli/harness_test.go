package cli

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChooseHarness(t *testing.T) {
	for _, tc := range []struct {
		answer, detected, want string
		fail                   bool
	}{
		{"\n", "claude-code", "claude-code", false},
		{"none\n", "cursor", "", false},
		{"windsurf\n", "none", "windsurf", false},
		{"invalid\n", "none", "", true},
	} {
		var out bytes.Buffer
		got, err := chooseHarness(strings.NewReader(tc.answer), &out, tc.detected)
		if (err != nil) != tc.fail || got != tc.want {
			t.Fatalf("%#v: %q %v", tc, got, err)
		}
		if !strings.Contains(out.String(), "harness-managed") {
			t.Fatal("missing lifecycle instructions")
		}
	}
}

func TestDetectHarness(t *testing.T) {
	root := t.TempDir()
	if got := detectHarness(root); got != "none" {
		t.Fatal(got)
	}
	os.WriteFile(filepath.Join(root, "CLAUDE.md"), nil, 0o644)
	if got := detectHarness(root); got != "claude-code" {
		t.Fatal(got)
	}
	os.Mkdir(filepath.Join(root, ".cursor"), 0o755)
	if got := detectHarness(root); got != "none" {
		t.Fatal("ambiguous detected harness", got)
	}
}

func TestSetupPromptsPreserveQueuedAnswers(t *testing.T) {
	input := bufio.NewReader(strings.NewReader("pnpm\nclaude-code\n"))
	var output bytes.Buffer
	invocation, err := chooseInvocation(input, &output, "global")
	if err != nil || invocation != "pnpm" {
		t.Fatalf("%s %v", invocation, err)
	}
	harness, err := chooseHarness(input, &output, "none")
	if err != nil || harness != "claude-code" {
		t.Fatalf("%s %v", harness, err)
	}
}
