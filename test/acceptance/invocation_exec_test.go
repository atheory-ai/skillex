package acceptance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
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
			var requests atomic.Int64
			registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				t.Logf("unexpected registry request %s", r.URL.Path)
				http.Error(w, "package fetching forbidden", http.StatusNotFound)
			}))
			t.Cleanup(registry.Close)
			global := t.TempDir()
			stub := []byte("#!/usr/bin/env node\nconsole.log('HOSTILE_GLOBAL_SKILLEX');\n")
			os.WriteFile(filepath.Join(global, "skillex"), stub, 0o755)
			os.MkdirAll(filepath.Join(global, "bin"), 0o755)
			os.WriteFile(filepath.Join(global, "bin", "skillex"), stub, 0o755)
			manifest := `{"name":"local-invocation-test","version":"1.0.0","devDependencies":{"@atheory-ai/skillex":"9.9.9"}}`
			if manager == "pnpm" {
				// A nonexistent package-manager pin and forced auto-install setting
				// must not cause either pnpm bootstrap or dependency acquisition.
				manifest = `{"name":"local-invocation-test","version":"1.0.0","packageManager":"pnpm@0.0.0","devDependencies":{"@atheory-ai/skillex":"9.9.9"}}`
				os.WriteFile(filepath.Join(dir, "pnpm-workspace.yaml"), []byte("verifyDepsBeforeRun: install\npmOnFail: download\n"), 0o644)
			}
			os.WriteFile(filepath.Join(dir, "package.json"), []byte(manifest), 0o644)
			os.WriteFile(filepath.Join(dir, ".npmrc"), []byte("registry="+registry.URL+"\n@atheory-ai:registry="+registry.URL+"\n"), 0o644)
			pkg := filepath.Join(dir, "node_modules", "@atheory-ai", "skillex")
			os.MkdirAll(filepath.Join(pkg, "bin"), 0o755)
			os.WriteFile(filepath.Join(pkg, "package.json"), []byte(`{"name":"@atheory-ai/skillex","version":"9.9.9","bin":{"skillex":"bin/skillex.js"}}`), 0o644)
			os.WriteFile(filepath.Join(pkg, "bin", "skillex.js"), []byte("#!/usr/bin/env node\nconsole.log('PINNED_LOCAL_9_9_9 ' + JSON.stringify(process.argv.slice(2))); if (process.argv[2] === 'exit7') process.exit(7);\n"), 0o755)
			bins := filepath.Join(dir, "node_modules", ".bin")
			os.MkdirAll(bins, 0o755)
			if err := os.Symlink("../@atheory-ai/skillex/bin/skillex.js", filepath.Join(bins, "skillex")); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			arguments := []string{"mcp", "argument with spaces", "--token=unchanged"}
			expected, err := json.Marshal(arguments)
			if err != nil {
				t.Fatal(err)
			}
			run := exec.CommandContext(ctx, command, append(args, arguments...)...)
			run.WaitDelay = 2 * time.Second
			run.Dir = dir
			run.Env = append(os.Environ(), "PATH="+global+string(os.PathListSeparator)+os.Getenv("PATH"), "npm_config_prefix="+global, "npm_config_cache="+cache, "npm_config_offline=true", "npm_config_registry="+registry.URL, "pnpm_config_registry="+registry.URL, "pnpm_config_store_dir="+filepath.Join(cache, "store"), "pnpm_config_state_dir="+filepath.Join(cache, "state"))
			output, err := run.CombinedOutput()
			if err != nil || !strings.Contains(string(output), "PINNED_LOCAL_9_9_9 "+string(expected)) {
				t.Fatalf("local command: %v %s", err, output)
			}
			exit := exec.CommandContext(ctx, command, append(args, "exit7")...)
			exit.Dir, exit.Env, exit.WaitDelay = dir, run.Env, 2*time.Second
			output, err = exit.CombinedOutput()
			if failure, ok := err.(*exec.ExitError); !ok || failure.ExitCode() != 7 {
				t.Fatalf("exit status must propagate: %v %s", err, output)
			}
			os.RemoveAll(filepath.Join(dir, "node_modules"))
			missing := exec.CommandContext(ctx, command, append(args, "mcp")...)
			missing.Dir = dir
			missing.Env = run.Env
			missing.WaitDelay = 2 * time.Second
			output, err = missing.CombinedOutput()
			if err == nil || strings.Contains(string(output), "PINNED_LOCAL_9_9_9") || strings.Contains(string(output), "HOSTILE_GLOBAL_SKILLEX") {
				t.Fatalf("missing install must fail: %v %s", err, output)
			}
			if requests.Load() != 0 {
				t.Fatalf("command attempted %d registry requests", requests.Load())
			}
			for _, name := range []string{"package-lock.json", "pnpm-lock.yaml", "pnpm-lock.yaml.env", "yarn.lock"} {
				if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
					t.Fatalf("command unexpectedly wrote %s: %v", name, err)
				}
			}
		})
	}
}
