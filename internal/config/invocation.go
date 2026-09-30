package config

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

// InstallConfig records how this project's pinned Skillex is invoked.
type InstallConfig struct {
	Strategy       string `yaml:"Strategy" json:"Strategy"`
	PackageManager string `yaml:"PackageManager,omitempty" json:"PackageManager,omitempty"`
	Binary         string `yaml:"Binary,omitempty" json:"Binary,omitempty"`
}

// Validate rejects ambiguous strategies and unsafe source paths.
func (i *InstallConfig) Validate() error {
	if i == nil {
		return nil
	}
	switch i.Strategy {
	case "global":
		if i.PackageManager != "" || i.Binary != "" {
			return fmt.Errorf("global installation requires no PackageManager or Binary")
		}
	case "local-dev-dependency":
		switch i.PackageManager {
		case "npm", "pnpm", "yarn-classic", "yarn-berry":
		default:
			return fmt.Errorf("Install.PackageManager must be npm, pnpm, yarn-classic, or yarn-berry")
		}
		if i.Binary != "" {
			return fmt.Errorf("Install.Binary is only supported for source")
		}
	case "source":
		path := filepath.Clean(i.Binary)
		if i.Binary == "" || filepath.IsAbs(path) || path == "." || path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) || strings.ContainsAny(i.Binary, "\r\n") {
			return fmt.Errorf("Install.Binary must be a relative binary path inside the project")
		}
		if i.PackageManager != "" {
			return fmt.Errorf("source installation requires no PackageManager")
		}
	default:
		return fmt.Errorf("unknown Install.Strategy %q", i.Strategy)
	}
	return nil
}

// Command returns an executable and argv prefix; it never enables package fetching.
// MCP harnesses run this command from the project root, just like the CLI examples.
func (i *InstallConfig) Command() (string, []string) {
	if i == nil || i.Strategy == "global" {
		return "skillex", nil
	}
	if i.Strategy == "source" {
		binary := filepath.ToSlash(filepath.Clean(i.Binary))
		if runtime.GOOS == "windows" && binary == ".skillex/bin/skillex" {
			binary += ".exe"
		}
		return "./" + binary, nil
	}
	switch i.PackageManager {
	case "npm":
		// npm exec can use a global or cached package when the local install is
		// missing. Running the installed wrapper directly fails closed instead.
		return "node", []string{"./node_modules/@atheory-ai/skillex/bin/skillex.js"}
	case "pnpm":
		// pnpm 11+ installs stale/missing dependencies before exec by default.
		// Disable that gate and automatic pnpm version downloads (v10 and v11+).
		// An explicit shim path also prevents fallback to a global skillex on PATH.
		binary := "./node_modules/.bin/skillex"
		if runtime.GOOS == "windows" {
			binary += ".cmd"
		}
		return "pnpm", []string{"--config.verify-deps-before-run=false", "--config.manage-package-manager-versions=false", "--config.pm-on-fail=ignore", "exec", binary}
	default:
		return "yarn", []string{"run", "skillex"}
	}
}

// CLI renders the command prefix for copyable shell instructions.
func (i *InstallConfig) CLI() string {
	command, args := i.Command()
	if strings.ContainsAny(command, " \t'\"$`;&|<>()*?!\\") {
		command = "'" + strings.ReplaceAll(command, "'", "'\\''") + "'"
	}
	return strings.Join(append([]string{command}, args...), " ")
}
