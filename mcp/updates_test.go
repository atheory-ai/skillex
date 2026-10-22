package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/atheory-ai/skillex/internal/updatecheck"
	mcplib "github.com/mark3labs/mcp-go/mcp"
)

func TestToolNoticesLiveInFirstTextBlockAndPreserveErrors(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "updates"), 0o700)
	data, _ := json.Marshal(map[string]any{"schema": 1, "source": "github", "latest_version": "0.11.0", "last_success_at": time.Now()})
	os.WriteFile(filepath.Join(dir, "updates", "github.json"), data, 0o600)
	checker := updatecheck.New(updatecheck.Options{Version: "0.10.0", Source: "github", CacheDir: dir, Offline: true})
	for _, isError := range []bool{false, true} {
		handler := withUpdateNotices(checker, func(context.Context, mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
			result := mcplib.NewToolResultText(`{"type":"no_match","vocabulary":{"total_skills":3}}`)
			result.IsError = isError
			return result, nil
		})
		result, err := handler(context.Background(), mcplib.CallToolRequest{})
		if err != nil || result.IsError != isError || len(result.Content) != 1 {
			t.Fatalf("result: %#v, %v", result, err)
		}
		text := result.Content[0].(mcplib.TextContent).Text
		if strings.Contains(text, "skillex_update_available") == isError {
			t.Fatalf("wrong notice for isError=%t: %s", isError, text)
		}
		var object map[string]json.RawMessage
		if json.Unmarshal([]byte(text), &object) != nil || string(object["type"]) != `"no_match"` {
			t.Fatal(text)
		}
	}
	if !strings.Contains(updateInstructions(checker.CachedNotices()), "restart or reconnect MCP") {
		t.Fatal("missing startup restart guidance")
	}
}
