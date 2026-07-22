package helpers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// Result holds the output of a skillex invocation.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// SkilexBinary returns the current checkout's development binary unless a test
// explicitly overrides it. Acceptance tests must never silently use a global or
// stale root-level binary.
func SkilexBinary() string {
	if v := os.Getenv("SKILLEX_BINARY"); v != "" {
		return v
	}
	name := "skillex"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(repoRoot(), ".skillex", "bin", name)
}

// BuildSkilexBinary builds the current checkout for acceptance tests that run
// directly through go test rather than a Make target.
func BuildSkilexBinary() error {
	binary := SkilexBinary()
	if os.Getenv("SKILLEX_BINARY") != "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
		return fmt.Errorf("creating development binary directory: %w", err)
	}
	cmd := exec.Command("go", "build", "-o", binary, "./cmd/skillex")
	cmd.Dir = repoRoot()
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("building development binary: %w\n%s", err, output)
	}
	return nil
}

// Run executes skillex in the given directory and returns stdout, stderr, and exit code.
// Never fails the test — the caller asserts on the results.
func Run(t *testing.T, dir string, args ...string) Result {
	t.Helper()
	cmd := exec.Command(SkilexBinary(), args...)
	cmd.Dir = dir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	return Result{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}
}

// RunJSON executes skillex with --json and unmarshals stdout into v.
// Fails the test if stdout is not valid JSON.
func RunJSON(t *testing.T, dir string, v interface{}, args ...string) Result {
	t.Helper()
	// Append --json if not already present
	hasJSON := false
	for _, a := range args {
		if a == "--json" {
			hasJSON = true
			break
		}
	}
	if !hasJSON {
		args = append(args, "--json")
	}

	result := Run(t, dir, args...)
	if result.Stdout != "" {
		if err := json.Unmarshal([]byte(result.Stdout), v); err != nil {
			t.Fatalf("RunJSON: stdout is not valid JSON: %v\nstdout: %s", err, result.Stdout)
		}
	} else {
		// Empty stdout means empty array/object — try zero value
		if result.ExitCode == 0 {
			// Set to zero value via json null or empty array
			if err := json.Unmarshal([]byte("null"), v); err != nil {
				// ignore
			}
		}
	}
	return result
}
