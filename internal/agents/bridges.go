package agents

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	claudeMarkerStart = "<!-- skillex:claude:start -->"
	claudeMarkerEnd   = "<!-- skillex:claude:end -->"
	geminiMarkerStart = "<!-- skillex:gemini:start -->"
	geminiMarkerEnd   = "<!-- skillex:gemini:end -->"
)

// UpdateBridgeFiles creates or updates small tool-specific bridge files when a
// project already appears to use a tool-specific instruction system.
func UpdateBridgeFiles(root string) ([]string, error) {
	var updated []string

	claudePath, claudeImport := detectClaudeBridge(root)
	if claudePath != "" {
		if err := updateBridgeFile(claudePath, claudeSection(claudeImport), claudeMarkerStart, claudeMarkerEnd); err != nil {
			return updated, err
		}
		updated = append(updated, claudePath)
	}

	geminiPath, geminiImport := detectGeminiBridge(root)
	if geminiPath != "" {
		if err := updateBridgeFile(geminiPath, geminiSection(geminiImport), geminiMarkerStart, geminiMarkerEnd); err != nil {
			return updated, err
		}
		updated = append(updated, geminiPath)
	}

	return updated, nil
}

func detectClaudeBridge(root string) (path, importPath string) {
	rootClaude := filepath.Join(root, "CLAUDE.md")
	if fileExists(rootClaude) || fileExists(filepath.Join(root, "CLAUDE.local.md")) {
		return rootClaude, "AGENTS.md"
	}

	claudeDir := filepath.Join(root, ".claude")
	if dirExists(claudeDir) {
		return filepath.Join(claudeDir, "CLAUDE.md"), "../AGENTS.md"
	}

	return "", ""
}

func detectGeminiBridge(root string) (path, importPath string) {
	rootGemini := filepath.Join(root, "GEMINI.md")
	if fileExists(rootGemini) || dirExists(filepath.Join(root, ".gemini")) {
		return rootGemini, "AGENTS.md"
	}
	return "", ""
}

func updateBridgeFile(path string, section string, markerStart string, markerEnd string) error {
	var existing string
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", filepath.Base(path), err)
	}
	if err == nil {
		existing = string(data)
	}

	updated := section
	if existing != "" {
		updated = replaceMarkedSection(existing, section, markerStart, markerEnd)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(updated), 0o644)
}

func claudeSection(importPath string) string {
	return claudeMarkerStart + "\n" +
		"@" + importPath + "\n" +
		claudeMarkerEnd + "\n"
}

func geminiSection(importPath string) string {
	return geminiMarkerStart + "\n" +
		"@" + importPath + "\n" +
		geminiMarkerEnd + "\n"
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
