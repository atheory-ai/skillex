package acceptance

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/atheory-ai/skillex/internal/config"
)

func TestInvocation_ExecutesInstalledLocalBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("local Unix bin symlink fixture")
	}
	for _, manager := range []string{"npm", "pnpm", "yarn-classic"} {
		t.Run(manager, func(t *testing.T) {
			install := &config.InstallConfig{Strategy: "local-dev-dependency", PackageManager: manager}
			command, args := install.Command()
			if _, err := exec.LookPath(command); err != nil {
				t.Skip("package manager unavailable")
			}
			dir := t.TempDir()
			cache := t.TempDir()
			os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"local-invocation-test","version":"1.0.0","devDependencies":{"@atheory-ai/skillex":"9.9.9"}}`), 0o644)
			pkg := filepath.Join(dir, "node_modules", "@atheory-ai", "skillex")
			os.MkdirAll(pkg, 0o755)
			os.WriteFile(filepath.Join(pkg, "package.json"), []byte(`{"name":"@atheory-ai/skillex","version":"9.9.9","bin":{"skillex":"bin.js"}}`), 0o644)
			os.WriteFile(filepath.Join(pkg, "bin.js"), []byte("#!/usr/bin/env node\nconsole.log('PINNED_LOCAL_9_9_9 ' + process.argv.slice(2).join(' '));\n"), 0o755)
			bins := filepath.Join(dir, "node_modules", ".bin")
			os.MkdirAll(bins, 0o755)
			if err := os.Symlink("../@atheory-ai/skillex/bin.js", filepath.Join(bins, "skillex")); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			run := exec.CommandContext(ctx, command, append(args, "mcp")...)
			run.Dir = dir
			run.Env = append(os.Environ(), "npm_config_cache="+cache, "npm_config_offline=true", "npm_config_registry=http://127.0.0.1:1")
			output, err := run.CombinedOutput()
			if err != nil || !strings.Contains(string(output), "PINNED_LOCAL_9_9_9 mcp") {
				t.Fatalf("local command: %v %s", err, output)
			}
			os.RemoveAll(filepath.Join(dir, "node_modules"))
			missing := exec.CommandContext(ctx, command, append(args, "mcp")...)
			missing.Dir = dir
			missing.Env = run.Env
			output, err = missing.CombinedOutput()
			if err == nil || strings.Contains(string(output), "PINNED_LOCAL_9_9_9") {
				t.Fatalf("missing install must fail: %v %s", err, output)
			}
		})
	}
}
