package mcp

import (
	"context"
	"strings"

	"github.com/atheory-ai/skillex/internal/updatecheck"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func updateInstructions(notices []updatecheck.Notice) string {
	var text strings.Builder
	for _, notice := range notices {
		text.WriteString(notice.Message)
		if notice.UpdateCommand != "" {
			text.WriteString(" Suggested command: " + notice.UpdateCommand)
		} else {
			text.WriteString(" See " + notice.ReleaseURL)
		}
	}
	return text.String()
}

func withUpdateNotices(updates *updatecheck.Checker, handler server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		result, err := handler(ctx, req)
		if err != nil || result == nil || result.IsError || len(result.Content) == 0 {
			return result, err
		}
		if text, ok := result.Content[0].(mcplib.TextContent); ok {
			text.Text = string(updatecheck.AddNotices([]byte(text.Text), updates.Notices(ctx)))
			result.Content[0] = text
		}
		return result, nil
	}
}
