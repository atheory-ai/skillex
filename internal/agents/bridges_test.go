package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateBridgeFilesNoMarkersDoesNothing(t *testing.T) {
	root := t.TempDir()

	updated, err := UpdateBridgeFiles(root)
	if err != nil {
		t.Fatalf("UpdateBridgeFiles() error = %v", err)
	}
	if len(updated) != 0 {
		t.Fatalf("updated = %v, want none", updated)
	}

	for _, name := range []string{"CLAUDE.md", "GEMINI.md"} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("%s should not be created without markers", name)
		}
	}
}

func TestUpdateBridgeFilesUpdatesExistingClaudeMd(t *testing.T) {
	root := t.TempDir()
	claudePath := filepath.Join(root, "CLAUDE.md")
	writeAgentTestFile(t, claudePath, "# Project\n\nKeep this.\n")

	updated, err := UpdateBridgeFiles(root)
	if err != nil {
		t.Fatalf("UpdateBridgeFiles() error = %v", err)
	}
	if len(updated) != 1 || updated[0] != claudePath {
		t.Fatalf("updated = %v, want [%s]", updated, claudePath)
	}

	content := readAgentTestFile(t, claudePath)
	if !strings.Contains(content, "Keep this.") {
		t.Fatalf("existing content not preserved:\n%s", content)
	}
	if !strings.Contains(content, "@AGENTS.md") {
		t.Fatalf("CLAUDE.md missing AGENTS import:\n%s", content)
	}
	if strings.Count(content, claudeMarkerStart) != 1 {
		t.Fatalf("CLAUDE.md should contain one managed section:\n%s", content)
	}
}

func TestUpdateBridgeFilesClaudeDirUsesRelativeImport(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	updated, err := UpdateBridgeFiles(root)
	if err != nil {
		t.Fatalf("UpdateBridgeFiles() error = %v", err)
	}
	claudePath := filepath.Join(root, ".claude", "CLAUDE.md")
	if len(updated) != 1 || updated[0] != claudePath {
		t.Fatalf("updated = %v, want [%s]", updated, claudePath)
	}

	content := readAgentTestFile(t, claudePath)
	if !strings.Contains(content, "@../AGENTS.md") {
		t.Fatalf(".claude/CLAUDE.md missing relative AGENTS import:\n%s", content)
	}
}

func TestUpdateBridgeFilesUpdatesExistingGeminiMd(t *testing.T) {
	root := t.TempDir()
	geminiPath := filepath.Join(root, "GEMINI.md")
	writeAgentTestFile(t, geminiPath, "# Gemini\n\nKeep this.\n")

	updated, err := UpdateBridgeFiles(root)
	if err != nil {
		t.Fatalf("UpdateBridgeFiles() error = %v", err)
	}
	if len(updated) != 1 || updated[0] != geminiPath {
		t.Fatalf("updated = %v, want [%s]", updated, geminiPath)
	}

	content := readAgentTestFile(t, geminiPath)
	if !strings.Contains(content, "Keep this.") {
		t.Fatalf("existing content not preserved:\n%s", content)
	}
	if !strings.Contains(content, "@AGENTS.md") {
		t.Fatalf("GEMINI.md missing AGENTS import:\n%s", content)
	}
	if strings.Count(content, geminiMarkerStart) != 1 {
		t.Fatalf("GEMINI.md should contain one managed section:\n%s", content)
	}
}

func TestUpdateBridgeFilesIdempotent(t *testing.T) {
	root := t.TempDir()
	claudePath := filepath.Join(root, "CLAUDE.md")
	writeAgentTestFile(t, claudePath, "# Project\n")

	if _, err := UpdateBridgeFiles(root); err != nil {
		t.Fatalf("first UpdateBridgeFiles() error = %v", err)
	}
	first := readAgentTestFile(t, claudePath)
	if _, err := UpdateBridgeFiles(root); err != nil {
		t.Fatalf("second UpdateBridgeFiles() error = %v", err)
	}
	second := readAgentTestFile(t, claudePath)

	if first != second {
		t.Fatalf("bridge update is not idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func writeAgentTestFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
}

func readAgentTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return string(data)
}
