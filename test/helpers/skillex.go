package helpers

import (
	"bytes"
	"encoding/json"
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

// SkilexBinary returns the path to the skillex binary.
func SkilexBinary() string {
	if v := os.Getenv("SKILLEX_BINARY"); v != "" {
		return v
	}
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(file), "..", "..")
	// Try repo root binary first
	candidate := filepath.Join(root, "skillex")
	if _, err := os.Stat(candidate); err == nil {
		abs, _ := filepath.Abs(candidate)
		return abs
	}
	// Fall back to PATH
	return "skillex"
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
