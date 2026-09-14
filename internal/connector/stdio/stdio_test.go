package stdio

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/atheory-ai/skillex/internal/capability"
)

func TestNewFactoryRequiresAbsoluteCommand(t *testing.T) {
	_, err := NewFactory([]ServerConfig{{CanonicalName: "io.example/test", Version: "1.0.0", Command: "fake-mcp"}})
	if err == nil {
		t.Fatal("NewFactory accepted a PATH-resolved command")
	}
}

func TestFactoryRejectsUnconfiguredServerBeforeStartingProcess(t *testing.T) {
	factory, err := NewFactory(nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = factory.Open(context.Background(), capability.Capability{
		Server: capability.ServerVersion{Identity: capability.ServerIdentity{CanonicalName: "io.example/missing"}, Version: "1.0.0"},
	})
	if !errors.Is(err, ErrServerNotConfigured) {
		t.Fatalf("Open() error = %v, want %v", err, ErrServerNotConfigured)
	}
}

func TestFactoryInvokesVerifiedToolOverStdio(t *testing.T) {
	selected := stdioTestCapability(t)
	factory := stdioTestFactory(t, "normal", time.Second)
	opened, err := factory.Open(context.Background(), selected)
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()

	result, err := opened.CallTool(context.Background(), selected.Name, map[string]any{"title": "release"})
	if err != nil {
		t.Fatal(err)
	}
	toolResult, ok := result.(ToolResult)
	if !ok {
		t.Fatalf("result type = %T", result)
	}
	structured, ok := toolResult.StructuredContent.(map[string]any)
	if !ok || structured["ok"] != true {
		t.Fatalf("structured result = %#v", toolResult.StructuredContent)
	}
}

func TestFactoryContainsDownstreamProtocolFailures(t *testing.T) {
	tests := []struct {
		name       string
		mode       string
		timeout    time.Duration
		wantError  string
		wantExpiry bool
	}{
		{name: "malformed JSON-RPC", mode: "malformed", timeout: time.Second, wantError: "invalid JSON-RPC message"},
		{name: "oversized response", mode: "oversized", timeout: time.Second, wantError: "reading downstream MCP response"},
		{name: "crashed process", mode: "crash", timeout: time.Second, wantError: "helper crashed"},
		{name: "startup timeout", mode: "timeout", timeout: 50 * time.Millisecond, wantExpiry: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := stdioTestFactory(t, test.mode, test.timeout).Open(context.Background(), stdioTestCapability(t))
			if err == nil {
				t.Fatal("Open() succeeded")
			}
			if test.wantExpiry {
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("Open() error = %v, want deadline exceeded", err)
				}
				return
			}
			if !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Open() error = %v, want substring %q", err, test.wantError)
			}
		})
	}
}

func stdioTestCapability(t *testing.T) capability.Capability {
	t.Helper()
	selected, err := (capability.Capability{
		Server:          capability.ServerVersion{Identity: capability.ServerIdentity{CanonicalName: "io.example/helper"}, Version: "1.0.0"},
		Kind:            capability.CapabilityTool,
		Name:            "issues.create",
		InputSchemaJSON: json.RawMessage(`{"type":"object"}`),
		Availability:    capability.AvailabilityReady,
	}).WithComputedSchemaDigest()
	if err != nil {
		t.Fatal(err)
	}
	return selected
}

func stdioTestFactory(t *testing.T, mode string, timeout time.Duration) *Factory {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	factory, err := NewFactory([]ServerConfig{{
		CanonicalName:  "io.example/helper",
		Version:        "1.0.0",
		Command:        binary,
		Args:           []string{"-test.run=TestStdioHelperProcess"},
		Environment:    []string{"SKILLEX_STDIO_HELPER=1", "SKILLEX_STDIO_HELPER_MODE=" + mode},
		StartupTimeout: timeout,
	}})
	if err != nil {
		t.Fatal(err)
	}
	return factory
}

func TestStdioHelperProcess(t *testing.T) {
	if os.Getenv("SKILLEX_STDIO_HELPER") != "1" {
		return
	}
	mode := os.Getenv("SKILLEX_STDIO_HELPER_MODE")
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		if mode == "malformed" {
			fmt.Fprintln(os.Stdout, "{")
			return
		}
		if mode == "oversized" {
			fmt.Fprintln(os.Stdout, strings.Repeat("x", maxMessageBytes+1))
			return
		}
		if mode == "crash" {
			fmt.Fprintln(os.Stderr, "helper crashed")
			os.Exit(17)
		}
		if mode == "timeout" {
			time.Sleep(5 * time.Second)
			return
		}

		var request rpcRequest
		if err := json.Unmarshal(scanner.Bytes(), &request); err != nil {
			os.Exit(18)
		}
		response := map[string]any{"jsonrpc": "2.0", "id": request.ID}
		switch request.Method {
		case "server/discover":
			response["result"] = map[string]any{
				"resultType": "complete", "supportedVersions": []string{ProtocolVersion},
				"capabilities": map[string]any{"tools": map[string]any{}},
			}
		case "tools/list":
			response["result"] = map[string]any{
				"resultType": "complete",
				"tools":      []any{map[string]any{"name": "issues.create", "inputSchema": map[string]any{"type": "object"}}},
			}
		case "tools/call":
			response["result"] = map[string]any{
				"resultType": "complete", "structuredContent": map[string]any{"ok": true},
			}
		default:
			response["error"] = map[string]any{"code": -32601, "message": "method not found"}
		}
		if err := encoder.Encode(response); err != nil {
			os.Exit(19)
		}
	}
}
