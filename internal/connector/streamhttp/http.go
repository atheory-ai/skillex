// Package streamhttp implements the stateless MCP 2026-07-28 Streamable HTTP transport.
package streamhttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/atheory-ai/skillex/internal/broker"
	"github.com/atheory-ai/skillex/internal/capability"
	tooljsonschema "github.com/atheory-ai/skillex/internal/jsonschema"
)

const (
	ProtocolVersion = "2026-07-28"
	maxMessageBytes = 4 << 20
)

var (
	ErrServerNotConfigured = errors.New("downstream MCP server is not configured")
	ErrCapabilityMissing   = errors.New("downstream MCP capability is missing")
	ErrSchemaChanged       = errors.New("downstream MCP capability schema changed")
	ErrToolResult          = errors.New("downstream MCP tool returned an error")
	ErrToolResultInvalid   = errors.New("downstream MCP result does not satisfy the capability output schema")
)

type ServerConfig struct {
	CanonicalName string
	Version       string
	Endpoint      string
	Headers       map[string]string
	Timeout       time.Duration
	HTTPClient    *http.Client
}

type Factory struct {
	configs map[string]ServerConfig
}

func NewFactory(configs []ServerConfig) (*Factory, error) {
	indexed := make(map[string]ServerConfig, len(configs))
	for _, config := range configs {
		if config.CanonicalName == "" || config.Version == "" {
			return nil, errors.New("HTTP server canonical name and version are required")
		}
		parsed, err := url.Parse(config.Endpoint)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return nil, fmt.Errorf("invalid Streamable HTTP endpoint for %s", config.CanonicalName)
		}
		if parsed.Scheme != "https" && !(parsed.Scheme == "http" && isLoopbackHost(parsed.Hostname())) {
			return nil, fmt.Errorf("Streamable HTTP endpoint for %s must use HTTPS (HTTP is allowed only for loopback)", config.CanonicalName)
		}
		if config.Timeout <= 0 {
			config.Timeout = 30 * time.Second
		}
		config.Headers = cloneHeaders(config.Headers)
		key := serverKey(config.CanonicalName, config.Version)
		if _, duplicate := indexed[key]; duplicate {
			return nil, fmt.Errorf("duplicate HTTP configuration for %s@%s", config.CanonicalName, config.Version)
		}
		indexed[key] = config
	}
	return &Factory{configs: indexed}, nil
}

func (f *Factory) Open(ctx context.Context, selected capability.Capability) (broker.Connector, error) {
	config, ok := f.configs[serverKey(selected.Server.Identity.CanonicalName, selected.Server.Version)]
	if !ok {
		return nil, fmt.Errorf("%w: %s@%s", ErrServerNotConfigured, selected.Server.Identity.CanonicalName, selected.Server.Version)
	}
	httpClient := http.Client{Timeout: config.Timeout}
	if config.HTTPClient != nil {
		httpClient = *config.HTTPClient
		httpClient.Timeout = config.Timeout
	}
	httpClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	client := &client{config: config, http: &httpClient}
	if err := client.discover(ctx); err != nil && !errors.Is(err, errMethodNotFound) {
		return nil, err
	}
	tools, err := client.listTools(ctx)
	if err != nil {
		return nil, err
	}
	tool, ok := tools[selected.Name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrCapabilityMissing, selected.Name)
	}
	digest, err := capability.ComputeSchemaDigest(capability.CapabilityTool, tool.Name, tool.InputSchema, tool.OutputSchema)
	if err != nil {
		return nil, err
	}
	if digest != selected.SchemaDigest {
		return nil, fmt.Errorf("%w: %s", ErrSchemaChanged, selected.Name)
	}
	client.tools = tools
	client.outputSchema = append(json.RawMessage(nil), selected.OutputSchemaJSON...)
	return client, nil
}

var errMethodNotFound = errors.New("downstream MCP method not found")

type client struct {
	config       ServerConfig
	http         *http.Client
	tools        map[string]toolDefinition
	outputSchema json.RawMessage
	nextID       atomic.Int64
}

func (c *client) discover(ctx context.Context) error {
	var result discoverResult
	if err := c.roundTrip(ctx, "server/discover", nil, &result); err != nil {
		return err
	}
	found := false
	for _, version := range result.SupportedVersions {
		found = found || version == ProtocolVersion
	}
	if !found {
		return fmt.Errorf("downstream MCP server does not advertise protocol %s", ProtocolVersion)
	}
	return nil
}

func (c *client) listTools(ctx context.Context) (map[string]toolDefinition, error) {
	tools := map[string]toolDefinition{}
	cursor := ""
	for page := 0; page < 32; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var result listToolsResult
		if err := c.roundTrip(ctx, "tools/list", params, &result); err != nil {
			return nil, err
		}
		for _, tool := range result.Tools {
			if tool.Name == "" {
				return nil, errors.New("downstream MCP tool has an empty name")
			}
			if _, duplicate := tools[tool.Name]; duplicate {
				return nil, fmt.Errorf("duplicate downstream MCP tool %q", tool.Name)
			}
			tools[tool.Name] = tool
		}
		if result.NextCursor == "" {
			return tools, nil
		}
		cursor = result.NextCursor
	}
	return nil, errors.New("downstream MCP tools/list exceeded 32 pages")
}

func (c *client) CallTool(ctx context.Context, name string, arguments map[string]any) (any, error) {
	if _, ok := c.tools[name]; !ok {
		return nil, fmt.Errorf("%w: %s", ErrCapabilityMissing, name)
	}
	if arguments == nil {
		arguments = map[string]any{}
	}
	var result ToolResult
	if err := c.roundTrip(ctx, "tools/call", map[string]any{"name": name, "arguments": arguments}, &result); err != nil {
		return nil, err
	}
	if result.IsError {
		return result, ErrToolResult
	}
	if err := tooljsonschema.Validate(c.outputSchema, result.StructuredContent); err != nil {
		return nil, ErrToolResultInvalid
	}
	return result, nil
}

func (c *client) Close() error { return nil }

func (c *client) roundTrip(ctx context.Context, method string, params map[string]any, target any) error {
	id := c.nextID.Add(1)
	if params == nil {
		params = map[string]any{}
	}
	params["_meta"] = map[string]any{
		"io.modelcontextprotocol/protocolVersion":    ProtocolVersion,
		"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "skillex", "version": "dev"},
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}
	payload, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("MCP-Protocol-Version", ProtocolVersion)
	request.Header.Set("Mcp-Method", method)
	if name, _ := params["name"].(string); name != "" {
		request.Header.Set("Mcp-Name", name)
	}
	for name, value := range c.config.Headers {
		request.Header.Set(name, value)
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("calling downstream MCP HTTP endpoint: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 && response.StatusCode < 400 {
		return errors.New("downstream MCP HTTP redirect refused")
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("downstream MCP HTTP status %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxMessageBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxMessageBytes {
		return errors.New("downstream MCP response exceeds message limit")
	}
	var rpc rpcResponse
	if err := json.Unmarshal(data, &rpc); err != nil {
		return errors.New("downstream MCP returned invalid JSON-RPC")
	}
	if rpc.ID != id {
		return errors.New("downstream MCP response id mismatch")
	}
	if rpc.Error != nil {
		if rpc.Error.Code == -32601 {
			return errMethodNotFound
		}
		return fmt.Errorf("downstream MCP error %d", rpc.Error.Code)
	}
	return json.Unmarshal(rpc.Result, target)
}

func serverKey(name, version string) string { return name + "\x00" + version }

func cloneHeaders(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for name, value := range source {
		result[name] = value
	}
	return result
}

func isLoopbackHost(host string) bool {
	host = strings.ToLower(host)
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

type ToolResult struct {
	ResultType        string          `json:"resultType"`
	Content           json.RawMessage `json:"content,omitempty"`
	StructuredContent any             `json:"structuredContent,omitempty"`
	IsError           bool            `json:"isError,omitempty"`
}

type toolDefinition struct {
	Name         string          `json:"name"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
}

type discoverResult struct {
	SupportedVersions []string `json:"supportedVersions"`
}

type listToolsResult struct {
	Tools      []toolDefinition `json:"tools"`
	NextCursor string           `json:"nextCursor,omitempty"`
}

type rpcRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      int64          `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params"`
}

type rpcResponse struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code int `json:"code"`
	} `json:"error,omitempty"`
}
