package helpers

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync/atomic"
	"testing"
)

// MCPResource represents a resource listed by the MCP server.
type MCPResource struct {
	URI      string `json:"uri"`
	Name     string `json:"name"`
	MIMEType string `json:"mimeType"`
}

// MCPClient communicates with a skillex mcp process over stdio.
type MCPClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
	nextID atomic.Int64
	t      *testing.T
}

type jsonRPCRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// StartMCPServer starts a skillex mcp subprocess and performs the MCP handshake.
// Cleaned up when the test ends.
func StartMCPServer(t *testing.T, dir string) *MCPClient {
	t.Helper()
	cmd := exec.Command(SkilexBinary(), "mcp")
	cmd.Dir = dir

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("creating stdin pipe: %v", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("creating stdout pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("starting MCP server: %v", err)
	}

	client := &MCPClient{
		cmd:    cmd,
		stdin:  stdinPipe,
		reader: bufio.NewReader(stdoutPipe),
		t:      t,
	}

	t.Cleanup(func() { client.Close() })

	// Perform MCP handshake
	client.initialize()

	return client
}

func (c *MCPClient) initialize() {
	c.t.Helper()
	// Send initialize request
	c.call("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "test-client", "version": "0.0.1"},
	})
	// Send initialized notification (no response expected)
	c.notify("notifications/initialized", nil)
}

// call sends a JSON-RPC request and returns the raw result.
func (c *MCPClient) call(method string, params any) json.RawMessage {
	c.t.Helper()
	id := c.nextID.Add(1)
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
	data, err := json.Marshal(req)
	if err != nil {
		c.t.Fatalf("marshaling MCP request: %v", err)
	}
	if _, err := fmt.Fprintf(c.stdin, "%s\n", data); err != nil {
		c.t.Fatalf("writing MCP request: %v", err)
	}

	// Read response (skip notifications)
	for {
		line, err := c.reader.ReadBytes('\n')
		if err != nil {
			c.t.Fatalf("reading MCP response: %v", err)
		}
		var resp jsonRPCResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			continue // skip non-JSON lines
		}
		if resp.Method != "" {
			continue // skip server notifications
		}
		if resp.ID != id {
			continue // skip responses for other requests
		}
		if resp.Error != nil {
			c.t.Fatalf("MCP error for %s: %s", method, resp.Error.Message)
		}
		return resp.Result
	}
}

// notify sends a JSON-RPC notification (no response expected).
func (c *MCPClient) notify(method string, params any) {
	req := jsonRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
	data, _ := json.Marshal(req)
	fmt.Fprintf(c.stdin, "%s\n", data)
}

// ListResources returns the list of resources from the MCP server.
func (c *MCPClient) ListResources() ([]MCPResource, error) {
	c.t.Helper()
	raw := c.call("resources/list", map[string]any{})
	var result struct {
		Resources []MCPResource `json:"resources"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("parsing resources/list response: %w", err)
	}
	return result.Resources, nil
}

// ReadResource reads a resource by URI.
func (c *MCPClient) ReadResource(uri string) (string, error) {
	c.t.Helper()
	raw := c.call("resources/read", map[string]any{"uri": uri})
	var result struct {
		Contents []struct {
			URI  string `json:"uri"`
			Text string `json:"text"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("parsing resources/read response: %w", err)
	}
	if len(result.Contents) == 0 {
		return "", nil
	}
	return result.Contents[0].Text, nil
}

// CallTool calls a tool by name with the given parameters.
func (c *MCPClient) CallTool(name string, params map[string]interface{}) (json.RawMessage, error) {
	c.t.Helper()
	raw := c.call("tools/call", map[string]any{
		"name":      name,
		"arguments": params,
	})
	return raw, nil
}

// CallToolText calls a tool and returns the text content of the result.
func (c *MCPClient) CallToolText(name string, params map[string]interface{}) (string, error) {
	c.t.Helper()
	raw, err := c.CallTool(name, params)
	if err != nil {
		return "", err
	}
	var result struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", fmt.Errorf("parsing tool result: %w", err)
	}
	if result.IsError {
		return "", fmt.Errorf("tool returned error")
	}
	if len(result.Content) == 0 {
		return "", nil
	}
	return result.Content[0].Text, nil
}

// Close shuts down the MCP server process.
func (c *MCPClient) Close() {
	c.stdin.Close()
	c.cmd.Wait()
}
