package acceptance

import (
	"strings"
	"testing"
	"time"

	"github.com/atheory-ai/skillex/test/helpers"
)

func TestMCP_ServerStarts(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	client := helpers.StartMCPServer(t, dir)
	defer client.Close()
	// If we get here, the handshake succeeded
}

func TestMCP_ResourceListing(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	client := helpers.StartMCPServer(t, dir)
	defer client.Close()

	resources, err := client.ListResources()
	if err != nil {
		t.Fatalf("listing resources: %v", err)
	}
	if len(resources) == 0 {
		t.Error("expected resources from MCP server, got none")
	}
	for _, r := range resources {
		if r.URI == "" {
			t.Error("resource has empty URI")
		}
	}
}

func TestMCP_ResourceContent(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	client := helpers.StartMCPServer(t, dir)
	defer client.Close()

	resources, err := client.ListResources()
	if err != nil {
		t.Fatalf("listing resources: %v", err)
	}

	// Find a resource for components.md
	var compURI string
	for _, r := range resources {
		if strings.Contains(r.URI, "components") {
			compURI = r.URI
			break
		}
	}
	if compURI == "" {
		t.Skip("components.md resource not found in MCP resource list")
	}

	content, err := client.ReadResource(compURI)
	if err != nil {
		t.Fatalf("reading resource: %v", err)
	}
	if !strings.Contains(content, "@test/ui") {
		t.Errorf("resource content should contain '@test/ui', got: %q", content[:minInt(200, len(content))])
	}
}

func TestMCP_QueryToolBasic(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	client := helpers.StartMCPServer(t, dir)
	defer client.Close()

	text, err := client.CallToolText("skillex_query", map[string]interface{}{
		"path":   "packages/app-a/src/auth.ts",
		"format": "summary",
	})
	if err != nil {
		t.Fatalf("skillex_query tool failed: %v", err)
	}
	if text == "" {
		t.Error("expected non-empty query result")
	}
}

func TestMCP_QueryToolComposition(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	client := helpers.StartMCPServer(t, dir)
	defer client.Close()

	text, err := client.CallToolText("skillex_query", map[string]interface{}{
		"path":   "packages/app-a/**",
		"topic":  "migration",
		"tags":   "breaking-change",
		"format": "summary",
	})
	if err != nil {
		t.Fatalf("skillex_query failed: %v", err)
	}
	if !strings.Contains(text, "migrations") {
		t.Errorf("expected migrations.md in result, got: %q", text)
	}
}

func TestMCP_GracefulShutdown(t *testing.T) {
	dir := helpers.CopyFixture(t, "monorepo-pnpm")
	helpers.Run(t, dir, "refresh")

	client := helpers.StartMCPServer(t, dir)

	done := make(chan struct{})
	go func() {
		client.Close()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(5 * time.Second):
		t.Error("MCP server did not shut down within 5 seconds")
	}
}
