package scanner

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// PackageJSON represents the Node package.json fields used by the Node resolver.
type PackageJSON struct {
	Name            string            `json:"name"`
	Version         string            `json:"version"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	Skillex         json.RawMessage   `json:"skillex"`
}

// SkilexExport holds the skillex config extracted from a package.json.
type SkilexExport struct {
	Enabled  bool
	Path     string // custom path, defaults to "skillex"
	PackPath string
}

// readPackageJSON parses a package.json file.
func readPackageJSON(path string) (*PackageJSON, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pkg PackageJSON
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil, err
	}
	if pkg.Dependencies == nil {
		pkg.Dependencies = map[string]string{}
	}
	if pkg.DevDependencies == nil {
		pkg.DevDependencies = map[string]string{}
	}
	return &pkg, nil
}

// parseSkilexExport extracts the skillex export config from a package.json skillex field.
func parseSkilexExport(raw json.RawMessage) SkilexExport {
	if raw == nil {
		return SkilexExport{}
	}
	s := strings.TrimSpace(string(raw))
	if s == "true" {
		return SkilexExport{Enabled: true, Path: "skillex"}
	}
	if s == "false" || s == "null" {
		return SkilexExport{}
	}
	// Try object form: {"path": "docs/skillex"}
	var obj struct {
		Path string `json:"path"`
		Pack string `json:"pack"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && (obj.Path != "" || obj.Pack != "") {
		export := SkilexExport{Enabled: true, Path: obj.Path, PackPath: obj.Pack}
		if export.Path == "" {
			export.Path = "skillex"
		}
		return export
	}
	return SkilexExport{}
}

// findNodeModules walks up from the given directory to find node_modules.
func findNodeModules(start string) string {
	dir := start
	for {
		nm := filepath.Join(dir, "node_modules")
		if info, err := os.Stat(nm); err == nil && info.IsDir() {
			return nm
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return filepath.Join(start, "node_modules")
}
