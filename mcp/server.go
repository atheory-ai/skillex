package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/atheory-ai/skillex/internal/broker"
	"github.com/atheory-ai/skillex/internal/brokerruntime"
	"github.com/atheory-ai/skillex/internal/capability"
	"github.com/atheory-ai/skillex/internal/config"
	"github.com/atheory-ai/skillex/internal/mcperror"
	"github.com/atheory-ai/skillex/internal/query"
	"github.com/atheory-ai/skillex/internal/registry"
)

// Serve starts the MCP server using stdio transport.
func Serve(reg *registry.Registry, version string) error {
	return serve(reg, query.New(reg), nil, version)
}

// ServeConfigured starts Skillex with additive capability discovery and broker
// tools when the project explicitly opted in.
func ServeConfigured(reg *registry.Registry, cfg *config.Config, root, version string) error {
	if cfg == nil || !cfg.MCPEnabled() {
		return Serve(reg, version)
	}
	runtime, err := brokerruntime.NewDiscovery(root, cfg, reg)
	if err != nil {
		return err
	}
	engine := query.NewWithCapabilities(reg, runtime.Broker, runtime.ContextDigest, runtime.View)
	return serve(reg, engine, runtime, version)
}

func serve(reg *registry.Registry, engine *query.Engine, runtime *brokerruntime.Runtime, version string) error {
	s := server.NewMCPServer(
		"skillex",
		version,
		server.WithToolCapabilities(true),
		server.WithResourceCapabilities(true, true),
	)

	// Register query tool
	queryTool := mcplib.NewTool(
		"skillex_query",
		mcplib.WithDescription(
			"Query skillex skills by path, topic, tags, package, or keyword search. "+
				"Use 'search' for intent-based discovery when you don't know the skill taxonomy — "+
				"pass space or comma-separated concepts and all matching skills are returned in one call. "+
				"Use topic/tags for structured filtering when you know the organization. "+
				"Returns skill content or metadata for agent consumption.",
		),
		mcplib.WithString("path",
			mcplib.Description("File path or glob pattern to scope the query"),
		),
		mcplib.WithString("topic",
			mcplib.Description("Comma-separated topic filters"),
		),
		mcplib.WithString("tags",
			mcplib.Description("Comma-separated tag filters"),
		),
		mcplib.WithString("package",
			mcplib.Description("Package name filter (e.g. @acme/foo)"),
		),
		mcplib.WithString("search",
			mcplib.Description(
				"Keyword search across skill names and descriptions. "+
					"Space or comma-separated terms are each matched independently — "+
					"use this to find skills by concept when you don't know the topic/tag taxonomy. "+
					"Example: 'search card pagination' finds all skills related to any of those terms.",
			),
		),
		mcplib.WithString("format",
			mcplib.Description("Output format: 'summary'. Queries never return skill content; use skillex_read for bounded targeted retrieval."),
			mcplib.Enum("summary"),
		),
		mcplib.WithNumber("limit", mcplib.Description("Maximum discovery results (1-20, default 8)")),
		mcplib.WithString("cursor", mcplib.Description("Continuation cursor returned by a previous query")),
		mcplib.WithString("mcp_server", mcplib.Description("Canonical downstream MCP server identity filter")),
		mcplib.WithString("mcp_kind", mcplib.Description("Capability kind filter: tool, prompt, resource-template, or server")),
		mcplib.WithString("mcp_availability", mcplib.Description("Capability readiness filter")),
	)

	s.AddTool(queryTool, func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		return handleQuery(engine, req)
	})
	readTool := mcplib.NewTool("skillex_read",
		mcplib.WithDescription("Read one selected Skillex skill or Markdown section within a byte budget. Always discover and narrow with skillex_query first."),
		mcplib.WithString("ref", mcplib.Required(), mcplib.Description("Skill ref returned by skillex_query")),
		mcplib.WithString("section", mcplib.Description("Optional section id returned by skillex_query")),
		mcplib.WithNumber("max_bytes", mcplib.Description("Maximum content bytes to return (default 24576, max 65536)")),
	)
	s.AddTool(readTool, func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		return handleRead(reg, req)
	})
	if runtime != nil {
		describeTool := mcplib.NewTool("skillex_mcp_describe",
			mcplib.WithDescription("Describe one selected downstream MCP capability from a ref returned by skillex_query. This is offline and does not connect to the downstream server."),
			mcplib.WithString("ref", mcplib.Required(), mcplib.Description("Capability ref returned by skillex_query")),
			mcplib.WithNumber("max_bytes", mcplib.Description("Maximum encoded description bytes (default 24576, max 65536)")),
		)
		s.AddTool(describeTool, func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
			return handleCapabilityDescribe(ctx, runtime, req)
		})
		callTool := mcplib.NewTool("skillex_mcp_call",
			mcplib.WithDescription("Invoke one explicitly selected downstream MCP tool, prompt, or resource template. Skillex revalidates context, schema, readiness, and policy before opening the selected server."),
			mcplib.WithString("ref", mcplib.Required(), mcplib.Description("Capability ref returned by skillex_query")),
			mcplib.WithObject("arguments", mcplib.Description("Arguments for the selected downstream tool")),
			mcplib.WithObject("input_responses", mcplib.Description("MRTR responses keyed by the downstream input request identifiers")),
			mcplib.WithString("request_state", mcplib.Description("Opaque requestState returned by an input_required result; echo byte-for-byte")),
		)
		s.AddTool(callTool, func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
			return handleCapabilityCall(ctx, runtime, req)
		})
	}

	// Register resources for each skill
	skills, err := reg.AllSkills()
	if err != nil {
		return fmt.Errorf("loading skills for MCP resources: %w", err)
	}

	for _, skill := range skills {
		sk := skill // capture loop variable
		uri := skillURI(sk)
		resource := mcplib.NewResource(
			uri,
			sk.Path,
			mcplib.WithResourceDescription(skillDescription(sk)),
			mcplib.WithMIMEType("text/markdown"),
		)
		s.AddResource(resource, func(ctx context.Context, req mcplib.ReadResourceRequest) ([]mcplib.ResourceContents, error) {
			return []mcplib.ResourceContents{
				mcplib.TextResourceContents{
					URI:      uri,
					MIMEType: "text/markdown",
					Text:     resourceSummary(sk),
				},
			}, nil
		})
	}

	return server.ServeStdio(s)
}

func handleQuery(eng *query.Engine, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	pathVal, _ := req.Params.Arguments["path"].(string)
	topicVal, _ := req.Params.Arguments["topic"].(string)
	tagsVal, _ := req.Params.Arguments["tags"].(string)
	pkgVal, _ := req.Params.Arguments["package"].(string)
	searchVal, _ := req.Params.Arguments["search"].(string)
	formatVal, _ := req.Params.Arguments["format"].(string)
	limitVal, _ := req.Params.Arguments["limit"].(float64)
	cursorVal, _ := req.Params.Arguments["cursor"].(string)
	serverVal, _ := req.Params.Arguments["mcp_server"].(string)
	kindVal, _ := req.Params.Arguments["mcp_kind"].(string)
	availabilityVal, _ := req.Params.Arguments["mcp_availability"].(string)

	var topics []string
	for _, t := range strings.Split(topicVal, ",") {
		if t = strings.TrimSpace(t); t != "" {
			topics = append(topics, t)
		}
	}

	var tags []string
	for _, t := range strings.Split(tagsVal, ",") {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}

	var format query.Format
	switch formatVal {
	case "content":
		format = query.FormatSummary
	case "summary":
		format = query.FormatSummary
	default:
		format = query.FormatDefault
	}

	resp, err := eng.Execute(query.Params{
		Path:           pathVal,
		Topics:         topics,
		Tags:           tags,
		Package:        pkgVal,
		Search:         searchVal,
		Format:         format,
		Limit:          int(limitVal),
		Cursor:         cursorVal,
		Server:         serverVal,
		CapabilityKind: kindVal,
		Availability:   availabilityVal,
	})
	if err != nil {
		return &mcplib.CallToolResult{
			Content: []mcplib.Content{
				mcplib.TextContent{Type: "text", Text: fmt.Sprintf("query failed: %v", err)},
			},
			IsError: true,
		}, nil
	}

	switch resp.Type {
	case query.ResponseTypeResults:
		// Determine the effective format to decide how to render the results.
		effectiveFormat := format
		if effectiveFormat == query.FormatDefault {
			effectiveFormat = query.FormatSummary
		}
		var sb strings.Builder
		if effectiveFormat == query.FormatContent {
			sb.WriteString(query.ContentString(resp.Results))
		} else {
			data, err := json.MarshalIndent(resp, "", "  ")
			if err != nil {
				return nil, err
			}
			sb.Write(data)
		}
		return mcplib.NewToolResultText(sb.String()), nil

	case query.ResponseTypeVocabulary, query.ResponseTypeNoMatch:
		// Return the full structured response as JSON so MCP-consuming agents can
		// programmatically inspect topics/tags/packages without parsing free-form text.
		data, err := json.MarshalIndent(resp, "", "  ")
		if err != nil {
			return mcplib.NewToolResultText(fmt.Sprintf("failed to encode response: %v", err)), nil
		}
		return mcplib.NewToolResultText(string(data)), nil
	}

	return mcplib.NewToolResultText(""), nil
}

func handleCapabilityDescribe(ctx context.Context, runtime *brokerruntime.Runtime, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	ref, _ := req.Params.Arguments["ref"].(string)
	maxBytes, _ := req.Params.Arguments["max_bytes"].(float64)
	selected, err := runtime.Broker.Describe(ctx, ref, broker.RequestContext{ContextDigest: runtime.ContextDigest, View: runtime.View})
	if err != nil {
		return toolError(err), nil
	}
	data, err := capability.MarshalDescription(selected, int(maxBytes))
	if err != nil {
		return toolError(err), nil
	}
	return mcplib.NewToolResultText(string(data)), nil
}

func handleCapabilityCall(ctx context.Context, runtime *brokerruntime.Runtime, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	ref, _ := req.Params.Arguments["ref"].(string)
	arguments, _ := req.Params.Arguments["arguments"].(map[string]any)
	inputResponses, _ := req.Params.Arguments["input_responses"].(map[string]any)
	requestState, _ := req.Params.Arguments["request_state"].(string)
	result, err := runtime.Broker.CallWithInput(ctx, ref, arguments, inputResponses, requestState, broker.RequestContext{ContextDigest: runtime.ContextDigest, View: runtime.View})
	if err != nil {
		return toolError(err), nil
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, err
	}
	return mcplib.NewToolResultText(string(data)), nil
}

func toolError(err error) *mcplib.CallToolResult {
	encoded, marshalErr := json.Marshal(mcperror.From(err))
	if marshalErr != nil {
		encoded = []byte(`{"code":"TOOL_CALL_FAILED","message":"MCP broker call failed"}`)
	}
	return &mcplib.CallToolResult{Content: []mcplib.Content{mcplib.TextContent{Type: "text", Text: string(encoded)}}, IsError: true}
}

func handleRead(reg *registry.Registry, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	ref, _ := req.Params.Arguments["ref"].(string)
	section, _ := req.Params.Arguments["section"].(string)
	maxBytes, _ := req.Params.Arguments["max_bytes"].(float64)
	resp, err := query.New(reg).Read(ref, section, int(maxBytes))
	if err != nil {
		return &mcplib.CallToolResult{Content: []mcplib.Content{mcplib.TextContent{Type: "text", Text: err.Error()}}, IsError: true}, nil
	}
	data, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return nil, err
	}
	return mcplib.NewToolResultText(string(data)), nil
}

func skillURI(s registry.Skill) string {
	scope := ""
	if len(s.Scopes) > 0 {
		scope = s.Scopes[0]
	}
	pkg := s.PackageName
	if pkg == "" {
		pkg = "repo"
	}
	return fmt.Sprintf("skillex://skills/%s/%s/%s",
		strings.ReplaceAll(scope, "/**", ""),
		strings.ReplaceAll(pkg, "/", "_"),
		s.Path,
	)
}

func skillDescription(s registry.Skill) string {
	var parts []string
	if s.Name != "" {
		parts = append(parts, s.Name)
	}
	if s.Description != "" {
		desc := s.Description
		if len(desc) > 120 {
			desc = desc[:117] + "..."
		}
		parts = append(parts, desc)
	}
	parts = append(parts, fmt.Sprintf("visibility=%s", s.Visibility))
	if s.PackageName != "" {
		parts = append(parts, fmt.Sprintf("package=%s@%s", s.PackageName, s.PackageVersion))
	}
	return strings.Join(parts, " | ")
}

func resourceSummary(s registry.Skill) string {
	var sb strings.Builder
	sb.WriteString("# ")
	if s.Name != "" {
		sb.WriteString(s.Name)
	} else {
		sb.WriteString(s.Path)
	}
	sb.WriteString("\n\n")
	if s.Description != "" {
		sb.WriteString(s.Description)
		sb.WriteString("\n\n")
	}
	sb.WriteString(fmt.Sprintf("Skill ref: %s\nContent bytes: %d\n", skillRef(s.Path), len([]byte(s.Content))))
	sections := query.Sections(s.Content)
	if len(sections) > 0 {
		sb.WriteString("\nSections:\n")
		for _, section := range sections {
			sb.WriteString(fmt.Sprintf("- %s (`%s`)\n", section.Title, section.ID))
		}
	}
	sb.WriteString("\nUse skillex_read with the ref and optional section id to retrieve bounded content.\n")
	return sb.String()
}

func skillRef(path string) string {
	return "skill:" + base64.RawURLEncoding.EncodeToString([]byte(path))
}
