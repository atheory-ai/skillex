package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/atheory-ai/skillex/internal/config"
	"github.com/mattn/go-isatty"
)

func parseInvocation(value string) (*config.InstallConfig, error) {
	i := &config.InstallConfig{Strategy: "global"}
	switch value {
	case "", "global":
	case "npm", "pnpm", "yarn-classic", "yarn-berry":
		i.Strategy, i.PackageManager = "local-dev-dependency", value
	case "source":
		i.Strategy, i.Binary = "source", ".skillex/bin/skillex"
	default:
		return nil, fmt.Errorf("unknown invocation %q (global, npm, pnpm, yarn-classic, yarn-berry, source)", value)
	}
	return i, i.Validate()
}

func detectInvocation(root string) string {
	var pkg struct {
		DevDependencies map[string]string `json:"devDependencies"`
		PackageManager  string            `json:"packageManager"`
	}
	data, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err == nil && json.Unmarshal(data, &pkg) == nil && pkg.DevDependencies["@atheory-ai/skillex"] != "" {
		manager := strings.Split(pkg.PackageManager, "@")[0]
		if manager == "" {
			count := 0
			for _, entry := range []struct{ file, manager string }{{"pnpm-lock.yaml", "pnpm"}, {"package-lock.json", "npm"}, {"yarn.lock", "yarn"}} {
				if _, err := os.Stat(filepath.Join(root, entry.file)); err == nil {
					manager = entry.manager
					count++
				}
			}
			if count > 1 {
				return "global"
			}
			if count == 0 {
				manager = "npm"
				if _, err := os.Stat(filepath.Join(root, ".yarnrc.yml")); err == nil {
					manager = "yarn"
				}
			}
		}
		switch manager {
		case "npm", "pnpm":
			return manager
		case "yarn":
			if strings.HasPrefix(pkg.PackageManager, "yarn@1.") {
				return "yarn-classic"
			}
			if strings.HasPrefix(pkg.PackageManager, "yarn@") {
				return "yarn-berry"
			}
			if _, err := os.Stat(filepath.Join(root, ".yarnrc.yml")); err == nil {
				return "yarn-berry"
			}
			return "yarn-classic"
		}
		return "global"
	}
	executable, err := os.Executable()
	if err == nil {
		relative, err := filepath.Rel(root, executable)
		if err == nil && (relative == filepath.Join(".skillex", "bin", "skillex") || relative == filepath.Join(".skillex", "bin", "skillex.exe")) {
			return "source"
		}
	}
	return "global"
}

func terminalInput() bool {
	return isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd())
}

func chooseInvocation(in io.Reader, out io.Writer, detected string) (string, error) {
	fmt.Fprintf(out, "Invocation [global/npm/pnpm/yarn-classic/yarn-berry/source] (%s): ", detected)
	answer, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		answer = detected
	}
	if _, err := parseInvocation(answer); err != nil {
		return "", err
	}
	return answer, nil
}

func setupInvocation(root string, yes bool, selected string) error {
	explicit := selected != ""
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	if selected == "" && cfg.Install != nil {
		return nil
	}
	if selected == "" {
		selected = detectInvocation(root)
		if !yes && terminalInput() {
			selected, err = chooseInvocation(os.Stdin, os.Stderr, selected)
			if err != nil {
				return err
			}
		}
	}
	install, err := parseInvocation(selected)
	if err != nil {
		return err
	}
	// Old configs retain their bytes and implicit global behavior unless explicitly changed.
	if !explicit && cfg.Install == nil && selected == "global" {
		return nil
	}
	if cfg.Install != nil && *cfg.Install == *install {
		return nil
	}
	cfg.Install = install
	path, format, err := config.ResolvePath(root)
	if err != nil {
		return err
	}
	data, err := config.Marshal(cfg, format)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644) //nolint:gosec // G306: user-owned project config
}
