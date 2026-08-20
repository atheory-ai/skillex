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
	"sort"
	"strings"
	"sync"
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
	CanonicalName   string
	Version         string
	Endpoint        string
	Headers         map[string]string
	Timeout         time.Duration
	HTTPClient      *http.Client
	DefinitionCache *DefinitionCache
	CachePartition  string
}

// DefinitionCache retains only capability definitions, never credentials or
// results. Private entries are keyed by the caller-provided auth partition.
type DefinitionCache struct {
	mu      sync.Mutex
	entries map[string]cachedDefinitions
}

type cachedDefinitions struct {
	tools     map[string]toolDefinition
	expiresAt time.Time
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
		if parsed.Scheme != "https" && (parsed.Scheme != "http" || !isLoopbackHost(parsed.Hostname())) {
			return nil, fmt.Errorf("streamable HTTP endpoint for %s must use HTTPS (HTTP is allowed only for loopback)", config.CanonicalName)
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
	tools, cached := config.cachedTools()
	if selected.Kind != capability.CapabilityTool {
		cached = false
	}
	if !cached {
		if err := client.discover(ctx); err != nil && !errors.Is(err, errMethodNotFound) {
			return nil, err
		}
		if selected.Kind != capability.CapabilityTool {
			definitions, err := client.inspect(ctx, selected.Server)
			if err != nil {
				return nil, err
			}
			runtimeCapability, ok := definitions[string(selected.Kind)+"\x00"+selected.Name]
			if !ok {
				return nil, fmt.Errorf("%w: %s", ErrCapabilityMissing, selected.Name)
			}
			if runtimeCapability.SchemaDigest != selected.SchemaDigest {
				return nil, fmt.Errorf("%w: %s", ErrSchemaChanged, selected.Name)
			}
			client.outputSchema = append(json.RawMessage(nil), selected.OutputSchemaJSON...)
			return client, nil
		}
		var ttl time.Duration
		var scope string
		var err error
		tools, ttl, scope, err = client.listTools(ctx)
		if err != nil {
			return nil, err
		}
		config.storeTools(tools, ttl, scope)
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

// Inspect explicitly connects to one trusted HTTP server and returns its
// tool, prompt, and resource-template definitions for offline indexing.
func Inspect(ctx context.Context, config ServerConfig) ([]capability.Capability, error) {
	factory, err := NewFactory([]ServerConfig{config})
	if err != nil {
		return nil, err
	}
	normalized := factory.configs[serverKey(config.CanonicalName, config.Version)]
	httpClient := http.Client{Timeout: normalized.Timeout}
	if normalized.HTTPClient != nil {
		httpClient = *normalized.HTTPClient
		httpClient.Timeout = normalized.Timeout
	}
	httpClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	client := &client{config: normalized, http: &httpClient}
	if err := client.discover(ctx); err != nil && !errors.Is(err, errMethodNotFound) {
		return nil, err
	}
	indexed, err := client.inspect(ctx, capability.ServerVersion{Identity: capability.ServerIdentity{CanonicalName: config.CanonicalName}, Version: config.Version})
	if err != nil {
		return nil, err
	}
	result := make([]capability.Capability, 0, len(indexed))
	for _, selected := range indexed {
		result = append(result, selected)
	}
	sort.Slice(result, func(i, j int) bool {
		return string(result[i].Kind)+"\x00"+result[i].Name < string(result[j].Kind)+"\x00"+result[j].Name
	})
	return result, nil
}

var errMethodNotFound = errors.New("downstream MCP method not found")

type client struct {
	config       ServerConfig
	http         *http.Client
	tools        map[string]toolDefinition
	prompts      map[string]promptDefinition
	resources    map[string]resourceTemplateDefinition
	outputSchema json.RawMessage
	nextID       atomic.Int64
}

func (c *client) inspect(ctx context.Context, server capability.ServerVersion) (map[string]capability.Capability, error) {
	result := map[string]capability.Capability{}
	tools, _, _, err := c.listTools(ctx)
	if err != nil {
		return nil, err
	}
	c.tools = tools
	for _, tool := range tools {
		selected, err := (capability.Capability{Server: server, Kind: capability.CapabilityTool, Name: tool.Name,
			Title: tool.Title, Description: tool.Description, InputSchemaJSON: tool.InputSchema, OutputSchemaJSON: tool.OutputSchema,
			Availability: capability.AvailabilityReady}).WithComputedSchemaDigest()
		if err != nil {
			return nil, err
		}
		result[string(selected.Kind)+"\x00"+selected.Name] = selected
	}
	prompts, err := c.listPrompts(ctx)
	if err != nil && !errors.Is(err, errMethodNotFound) {
		return nil, err
	}
	c.prompts = prompts
	for _, prompt := range prompts {
		input, err := promptInputSchema(prompt.Arguments)
		if err != nil {
			return nil, err
		}
		selected, err := (capability.Capability{Server: server, Kind: capability.CapabilityPrompt, Name: prompt.Name,
			Title: prompt.Title, Description: prompt.Description, InputSchemaJSON: input,
			Availability: capability.AvailabilityReady}).WithComputedSchemaDigest()
		if err != nil {
			return nil, err
		}
		result[string(selected.Kind)+"\x00"+selected.Name] = selected
	}
	resources, err := c.listResourceTemplates(ctx)
	if err != nil && !errors.Is(err, errMethodNotFound) {
		return nil, err
	}
	c.resources = resources
	for _, resource := range resources {
		input := json.RawMessage(`{"type":"object","properties":{"uri":{"type":"string"}},"required":["uri"],"additionalProperties":false}`)
		selected, err := (capability.Capability{Server: server, Kind: capability.CapabilityResourceTemplate, Name: resource.URITemplate,
			Title: resource.Name, Description: resource.Description, InputSchemaJSON: input,
			Availability: capability.AvailabilityReady}).WithComputedSchemaDigest()
		if err != nil {
			return nil, err
		}
		result[string(selected.Kind)+"\x00"+selected.Name] = selected
	}
	return result, nil
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

func (c *client) listTools(ctx context.Context) (map[string]toolDefinition, time.Duration, string, error) {
	tools := map[string]toolDefinition{}
	cursor := ""
	var ttl time.Duration
	var cacheScope string
	for page := 0; page < 32; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var result listToolsResult
		if err := c.roundTrip(ctx, "tools/list", params, &result); err != nil {
			return nil, 0, "", err
		}
		if result.TTLMS < 0 || result.CacheScope != "" && result.CacheScope != "public" && result.CacheScope != "private" {
			return nil, 0, "", errors.New("downstream MCP returned invalid list cache metadata")
		}
		if page == 0 {
			ttl = time.Duration(result.TTLMS) * time.Millisecond
			cacheScope = result.CacheScope
		} else if time.Duration(result.TTLMS)*time.Millisecond != ttl || result.CacheScope != cacheScope {
			return nil, 0, "", errors.New("downstream MCP changed list cache metadata during pagination")
		}
		for _, tool := range result.Tools {
			if tool.Name == "" {
				return nil, 0, "", errors.New("downstream MCP tool has an empty name")
			}
			if _, duplicate := tools[tool.Name]; duplicate {
				return nil, 0, "", fmt.Errorf("duplicate downstream MCP tool %q", tool.Name)
			}
			tools[tool.Name] = tool
		}
		if result.NextCursor == "" {
			return tools, ttl, cacheScope, nil
		}
		cursor = result.NextCursor
	}
	return nil, 0, "", errors.New("downstream MCP tools/list exceeded 32 pages")
}

func (c *client) listPrompts(ctx context.Context) (map[string]promptDefinition, error) {
	result := map[string]promptDefinition{}
	cursor := ""
	for page := 0; page < 32; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var listed listPromptsResult
		if err := c.roundTrip(ctx, "prompts/list", params, &listed); err != nil {
			return nil, err
		}
		for _, prompt := range listed.Prompts {
			if prompt.Name == "" {
				return nil, errors.New("downstream MCP prompt has an empty name")
			}
			result[prompt.Name] = prompt
		}
		if listed.NextCursor == "" {
			return result, nil
		}
		cursor = listed.NextCursor
	}
	return nil, errors.New("downstream MCP prompts/list exceeded 32 pages")
}

func (c *client) listResourceTemplates(ctx context.Context) (map[string]resourceTemplateDefinition, error) {
	result := map[string]resourceTemplateDefinition{}
	cursor := ""
	for page := 0; page < 32; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var listed listResourceTemplatesResult
		if err := c.roundTrip(ctx, "resources/templates/list", params, &listed); err != nil {
			return nil, err
		}
		for _, resource := range listed.ResourceTemplates {
			if resource.URITemplate == "" {
				return nil, errors.New("downstream MCP resource template has an empty URI template")
			}
			result[resource.URITemplate] = resource
		}
		if listed.NextCursor == "" {
			return result, nil
		}
		cursor = listed.NextCursor
	}
	return nil, errors.New("downstream MCP resources/templates/list exceeded 32 pages")
}

func (c ServerConfig) cachedTools() (map[string]toolDefinition, bool) {
	if c.DefinitionCache == nil {
		return nil, false
	}
	c.DefinitionCache.mu.Lock()
	defer c.DefinitionCache.mu.Unlock()
	for _, key := range []string{serverKey(c.CanonicalName, c.Version) + "\x00public", serverKey(c.CanonicalName, c.Version) + "\x00private\x00" + c.CachePartition} {
		entry, ok := c.DefinitionCache.entries[key]
		if ok && time.Now().Before(entry.expiresAt) {
			return cloneTools(entry.tools), true
		}
		delete(c.DefinitionCache.entries, key)
	}
	return nil, false
}

func (c ServerConfig) storeTools(tools map[string]toolDefinition, ttl time.Duration, scope string) {
	if c.DefinitionCache == nil || ttl <= 0 || scope == "" {
		return
	}
	c.DefinitionCache.mu.Lock()
	defer c.DefinitionCache.mu.Unlock()
	if c.DefinitionCache.entries == nil {
		c.DefinitionCache.entries = map[string]cachedDefinitions{}
	}
	key := serverKey(c.CanonicalName, c.Version) + "\x00" + scope
	if scope == "private" {
		key += "\x00" + c.CachePartition
	}
	c.DefinitionCache.entries[key] = cachedDefinitions{tools: cloneTools(tools), expiresAt: time.Now().Add(ttl)}
}

func cloneTools(source map[string]toolDefinition) map[string]toolDefinition {
	result := make(map[string]toolDefinition, len(source))
	for name, tool := range source {
		tool.InputSchema = append(json.RawMessage(nil), tool.InputSchema...)
		tool.OutputSchema = append(json.RawMessage(nil), tool.OutputSchema...)
		result[name] = tool
	}
	return result
}

func (c *client) CallTool(ctx context.Context, name string, arguments map[string]any) (any, error) {
	return c.CallToolRound(ctx, name, arguments, nil, "")
}

func (c *client) CallToolRound(ctx context.Context, name string, arguments, inputResponses map[string]any, requestState string) (any, error) {
	if _, ok := c.tools[name]; !ok {
		return nil, fmt.Errorf("%w: %s", ErrCapabilityMissing, name)
	}
	if arguments == nil {
		arguments = map[string]any{}
	}
	var result ToolResult
	if err := c.roundTrip(ctx, "tools/call", roundParams(map[string]any{"name": name, "arguments": arguments}, inputResponses, requestState), &result); err != nil {
		return nil, err
	}
	if result.ResultType != "" && result.ResultType != "complete" {
		return result, nil
	}
	if result.IsError {
		return result, ErrToolResult
	}
	if err := tooljsonschema.Validate(c.outputSchema, result.StructuredContent); err != nil {
		return nil, ErrToolResultInvalid
	}
	return result, nil
}

func (c *client) GetPrompt(ctx context.Context, name string, arguments map[string]any) (any, error) {
	return c.GetPromptRound(ctx, name, arguments, nil, "")
}

func (c *client) GetPromptRound(ctx context.Context, name string, arguments, inputResponses map[string]any, requestState string) (any, error) {
	if _, ok := c.prompts[name]; !ok {
		return nil, fmt.Errorf("%w: %s", ErrCapabilityMissing, name)
	}
	var result map[string]any
	if err := c.roundTrip(ctx, "prompts/get", roundParams(map[string]any{"name": name, "arguments": arguments}, inputResponses, requestState), &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *client) ReadResource(ctx context.Context, uri string) (any, error) {
	return c.ReadResourceRound(ctx, uri, nil, "")
}

func (c *client) ReadResourceRound(ctx context.Context, uri string, inputResponses map[string]any, requestState string) (any, error) {
	var result map[string]any
	if err := c.roundTrip(ctx, "resources/read", roundParams(map[string]any{"uri": uri}, inputResponses, requestState), &result); err != nil {
		return nil, err
	}
	return result, nil
}

func roundParams(params map[string]any, inputResponses map[string]any, requestState string) map[string]any {
	if len(inputResponses) > 0 {
		params["inputResponses"] = inputResponses
	}
	if requestState != "" {
		params["requestState"] = requestState
	}
	return params
}

func promptInputSchema(arguments []promptArgument) (json.RawMessage, error) {
	properties := map[string]any{}
	var required []string
	for _, argument := range arguments {
		properties[argument.Name] = map[string]any{"type": "string", "description": argument.Description}
		if argument.Required {
			required = append(required, argument.Name)
		}
	}
	schema := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if len(required) > 0 {
		schema["required"] = required
	}
	return json.Marshal(schema)
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
	} else if uri, _ := params["uri"].(string); uri != "" {
		request.Header.Set("Mcp-Name", uri)
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
	InputRequests     json.RawMessage `json:"inputRequests,omitempty"`
	RequestState      string          `json:"requestState,omitempty"`
	Meta              json.RawMessage `json:"_meta,omitempty"`
}

type toolDefinition struct {
	Name         string          `json:"name"`
	Title        string          `json:"title,omitempty"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
}

type promptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

type promptDefinition struct {
	Name        string           `json:"name"`
	Title       string           `json:"title,omitempty"`
	Description string           `json:"description,omitempty"`
	Arguments   []promptArgument `json:"arguments,omitempty"`
}

type resourceTemplateDefinition struct {
	URITemplate string `json:"uriTemplate"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	MIMEType    string `json:"mimeType,omitempty"`
}

type discoverResult struct {
	SupportedVersions []string `json:"supportedVersions"`
}

type listToolsResult struct {
	Tools      []toolDefinition `json:"tools"`
	NextCursor string           `json:"nextCursor,omitempty"`
	TTLMS      int64            `json:"ttlMs,omitempty"`
	CacheScope string           `json:"cacheScope,omitempty"`
}

type listPromptsResult struct {
	Prompts    []promptDefinition `json:"prompts"`
	NextCursor string             `json:"nextCursor,omitempty"`
}

type listResourceTemplatesResult struct {
	ResourceTemplates []resourceTemplateDefinition `json:"resourceTemplates"`
	NextCursor        string                       `json:"nextCursor,omitempty"`
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
