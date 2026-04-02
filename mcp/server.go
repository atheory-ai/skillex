package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	mcplib "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/atheory-ai/skillex/internal/query"
	"github.com/atheory-ai/skillex/internal/registry"
)

// Serve starts the MCP server using stdio transport.
func Serve(reg *registry.Registry, version string) error {
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
			mcplib.Description("Output format: 'content' or 'summary' (default: summary when using search, content otherwise)"),
			mcplib.Enum("content", "summary"),
		),
	)

	s.AddTool(queryTool, func(ctx context.Context, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
		return handleQuery(reg, req)
	})

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
					Text:     sk.Content,
				},
			}, nil
		})
	}

	return server.ServeStdio(s)
}

func handleQuery(reg *registry.Registry, req mcplib.CallToolRequest) (*mcplib.CallToolResult, error) {
	pathVal, _ := req.Params.Arguments["path"].(string)
	topicVal, _ := req.Params.Arguments["topic"].(string)
	tagsVal, _ := req.Params.Arguments["tags"].(string)
	pkgVal, _ := req.Params.Arguments["package"].(string)
	searchVal, _ := req.Params.Arguments["search"].(string)
	formatVal, _ := req.Params.Arguments["format"].(string)

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
		format = query.FormatContent
	case "summary":
		format = query.FormatSummary
	default:
		format = query.FormatDefault
	}

	eng := query.New(reg)
	resp, err := eng.Execute(query.Params{
		Path:    pathVal,
		Topics:  topics,
		Tags:    tags,
		Package: pkgVal,
		Search:  searchVal,
		Format:  format,
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
			if searchVal != "" {
				effectiveFormat = query.FormatSummary
			} else {
				effectiveFormat = query.FormatContent
			}
		}
		var sb strings.Builder
		if effectiveFormat == query.FormatContent {
			sb.WriteString(query.ContentString(resp.Results))
		} else {
			for _, r := range resp.Results {
				sb.WriteString(fmt.Sprintf("**%s**\n", r.Path))
				if r.Name != "" {
					sb.WriteString(fmt.Sprintf("  Name: %s\n", r.Name))
				}
				if r.Description != "" {
					sb.WriteString(fmt.Sprintf("  Description: %s\n", r.Description))
				}
				if r.PackageName != "" {
					sb.WriteString(fmt.Sprintf("  Package: %s@%s\n", r.PackageName, r.PackageVersion))
				}
				sb.WriteString(fmt.Sprintf("  Visibility: %s\n", r.Visibility))
				if len(r.Topics) > 0 {
					sb.WriteString(fmt.Sprintf("  Topics: %s\n", strings.Join(r.Topics, ", ")))
				}
				if len(r.Tags) > 0 {
					sb.WriteString(fmt.Sprintf("  Tags: %s\n", strings.Join(r.Tags, ", ")))
				}
				sb.WriteString("\n")
			}
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
