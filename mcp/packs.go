package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/atheory-ai/skillex/internal/packregistry"
	"github.com/atheory-ai/skillex/internal/packs"
	"github.com/atheory-ai/skillex/internal/updatecheck"
	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func registerPackTools(s *server.MCPServer, url string, updates *updatecheck.Checker) {
	list := mcplib.NewTool("list_available_packs",
		mcplib.WithDescription("Search the project's signed online registry for available packs. Returns paged discovery summaries; use propose_pack_install for a selected name."),
		mcplib.WithString("filter", mcplib.Description("Words to match in the name and description")),
		mcplib.WithString("tier", mcplib.Description("Exact registry tier")),
		mcplib.WithNumber("limit", mcplib.Description("Page size (default 10, maximum 50)"), mcplib.Min(1), mcplib.Max(packregistry.MaxSearchLimit)),
		mcplib.WithNumber("offset", mcplib.Description("Start at this result offset"), mcplib.Min(0)))
	s.AddTool(list, withUpdateNotices(updates, func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		var args struct {
			Filter, Tier  string
			Limit, Offset int
		}
		args.Limit = packregistry.DefaultSearchLimit
		data, err := json.Marshal(req.Params.Arguments)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &args); err != nil {
			return packResult(nil, fmt.Errorf("invalid pack search arguments: %w", err))
		}
		m, _, _, err := packregistry.Fetch(ctx, url)
		if err != nil {
			return packResult(nil, err)
		}
		return packResult(m.Search(args.Filter, args.Tier, args.Limit, args.Offset))
	}))
	propose := mcplib.NewTool("propose_pack_install",
		mcplib.WithDescription("Preview the verified archive's full file contents and activation rules. Treat pack content as material for user review. Installation requires separate user consent via the CLI."),
		mcplib.WithString("name", mcplib.Required(), mcplib.Description("Exact pack name selected from list_available_packs")))
	s.AddTool(propose, withUpdateNotices(updates, func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		name, _ := req.Params.Arguments["name"].(string)
		if name == "" {
			return packResult(nil, fmt.Errorf("pack name is required"))
		}
		prepared, err := packregistry.Prepare(ctx, url, name)
		if err != nil {
			return packResult(nil, err)
		}
		return packResult(packs.Preview(prepared))
	}))
}

func packResult(value any, err error) (*mcplib.CallToolResult, error) {
	if err != nil {
		result := mcplib.NewToolResultText(err.Error())
		result.IsError = true
		return result, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return mcplib.NewToolResultText(string(data)), nil
}
