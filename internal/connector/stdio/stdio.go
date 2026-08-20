// Package stdio implements the modern MCP stdio transport behind the broker's
// protocol-neutral connector boundary.
package stdio

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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
	// ProtocolVersion is the first modern, stateless MCP revision supported by
	// this adapter.
	ProtocolVersion = "2026-07-28"
	maxMessageBytes = 4 << 20
	maxStderrBytes  = 64 << 10
)

var (
	ErrServerNotConfigured = errors.New("downstream MCP server is not configured")
	ErrProtocolUnsupported = errors.New("downstream MCP protocol version is unsupported")
	ErrCapabilityMissing   = errors.New("downstream MCP capability is missing")
	ErrSchemaChanged       = errors.New("downstream MCP capability schema changed")
	ErrToolResult          = errors.New("downstream MCP tool returned an error")
	ErrToolResultInvalid   = errors.New("downstream MCP result does not satisfy the capability output schema")
)

// ServerConfig is trusted launch configuration for one exact server version.
// Environment entries are an explicit allowlist; the parent environment is
// never inherited.
type ServerConfig struct {
	CanonicalName  string
	Version        string
	Command        string
	Args           []string
	Directory      string
	Environment    []string
	StartupTimeout time.Duration
}

// Factory opens exact configured stdio server versions lazily.
type Factory struct {
	configs map[string]ServerConfig
}

// NewFactory validates and indexes trusted launch configurations.
func NewFactory(configs []ServerConfig) (*Factory, error) {
	indexed := make(map[string]ServerConfig, len(configs))
	for _, config := range configs {
		if config.CanonicalName == "" || config.Version == "" {
			return nil, errors.New("stdio server canonical name and version are required")
		}
		if !filepath.IsAbs(config.Command) {
			return nil, fmt.Errorf("stdio command for %s must be absolute", config.CanonicalName)
		}
		if config.Directory != "" && !filepath.IsAbs(config.Directory) {
			return nil, fmt.Errorf("stdio directory for %s must be absolute", config.CanonicalName)
		}
		if config.StartupTimeout <= 0 {
			config.StartupTimeout = 5 * time.Second
		}
		key := serverKey(config.CanonicalName, config.Version)
		if _, exists := indexed[key]; exists {
			return nil, fmt.Errorf("duplicate stdio configuration for %s@%s", config.CanonicalName, config.Version)
		}
		config.Args = append([]string(nil), config.Args...)
		config.Environment = append([]string(nil), config.Environment...)
		indexed[key] = config
	}
	return &Factory{configs: indexed}, nil
}

// Open starts the selected server, negotiates modern MCP, lists its tools, and
// verifies the selected tool schema before returning a callable connector.
func (f *Factory) Open(ctx context.Context, selected capability.Capability) (broker.Connector, error) {
	config, ok := f.configs[serverKey(selected.Server.Identity.CanonicalName, selected.Server.Version)]
	if !ok {
		return nil, fmt.Errorf("%w: %s@%s", ErrServerNotConfigured, selected.Server.Identity.CanonicalName, selected.Server.Version)
	}
	client, err := start(ctx, config)
	if err != nil {
		return nil, err
	}

	startupCtx, cancel := context.WithTimeout(ctx, config.StartupTimeout)
	defer cancel()
	if err := client.discover(startupCtx); err != nil {
		_ = client.Close()
		return nil, err
	}
	tools, err := client.listTools(startupCtx)
	if err != nil {
		_ = client.Close()
		return nil, err
	}
	tool, ok := tools[selected.Name]
	if !ok {
		_ = client.Close()
		return nil, fmt.Errorf("%w: %s", ErrCapabilityMissing, selected.Name)
	}
	digest, err := capability.ComputeSchemaDigest(capability.CapabilityTool, tool.Name, tool.InputSchema, tool.OutputSchema)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("digesting runtime schema for %s: %w", tool.Name, err)
	}
	if digest != selected.SchemaDigest {
		_ = client.Close()
		return nil, fmt.Errorf("%w: %s", ErrSchemaChanged, selected.Name)
	}
	client.tools = tools
	client.outputSchema = append(json.RawMessage(nil), selected.OutputSchemaJSON...)
	return client, nil
}

func serverKey(name, version string) string { return name + "\x00" + version }

type client struct {
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	responses    chan rpcResponse
	readErr      chan error
	wait         chan error
	stderr       *limitedBuffer
	tools        map[string]toolDefinition
	outputSchema json.RawMessage
	nextID       atomic.Int64
	callMu       sync.Mutex
	closeOnce    sync.Once
	closeErr     error
}

func start(ctx context.Context, config ServerConfig) (*client, error) {
	// ServerConfig is accepted only from the trusted, exact-version connector
	// map; repository bindings cannot supply commands or arguments.
	//nolint:gosec // G204 is intentionally confined to trusted launch configuration.
	cmd := exec.CommandContext(ctx, config.Command, config.Args...)
	cmd.Dir = config.Directory
	// exec.Cmd treats a nil Env as "inherit everything". Allocate even when the
	// allowlist is empty so an unconfigured server receives no parent secrets.
	cmd.Env = make([]string, len(config.Environment))
	copy(cmd.Env, config.Environment)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("creating downstream stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("creating downstream stdout: %w", err)
	}
	stderr := &limitedBuffer{limit: maxStderrBytes}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting downstream MCP server: %w", err)
	}
	c := &client{
		cmd:       cmd,
		stdin:     stdin,
		responses: make(chan rpcResponse, 8),
		readErr:   make(chan error, 1),
		wait:      make(chan error, 1),
		stderr:    stderr,
	}
	go c.readLoop(stdout)
	go func() { c.wait <- cmd.Wait() }()
	return c, nil
}

func (c *client) discover(ctx context.Context) error {
	var result discoverResult
	if err := c.roundTrip(ctx, "server/discover", nil, &result); err != nil {
		return fmt.Errorf("discovering downstream MCP server: %w", err)
	}
	if result.ResultType != "complete" {
		return fmt.Errorf("%w: discovery result type %q", ErrProtocolUnsupported, result.ResultType)
	}
	found := false
	for _, version := range result.SupportedVersions {
		if version == ProtocolVersion {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("%w: server supports %v", ErrProtocolUnsupported, result.SupportedVersions)
	}
	if _, ok := result.Capabilities["tools"]; !ok {
		return fmt.Errorf("%w: server does not advertise tools", ErrCapabilityMissing)
	}
	return nil
}

func (c *client) listTools(ctx context.Context) (map[string]toolDefinition, error) {
	tools := make(map[string]toolDefinition)
	cursor := ""
	for page := 0; page < 32; page++ {
		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var result listToolsResult
		if err := c.roundTrip(ctx, "tools/list", params, &result); err != nil {
			return nil, fmt.Errorf("listing downstream MCP tools: %w", err)
		}
		if result.ResultType != "complete" {
			return nil, fmt.Errorf("unexpected tools/list result type %q", result.ResultType)
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

// CallTool invokes a tool that was present and schema-verified during Open.
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
	if result.ResultType != "complete" {
		return result, fmt.Errorf("unexpected tools/call result type %q", result.ResultType)
	}
	if result.IsError {
		return result, ErrToolResult
	}
	if err := tooljsonschema.Validate(c.outputSchema, result.StructuredContent); err != nil {
		return nil, ErrToolResultInvalid
	}
	return result, nil
}

// Close performs the MCP stdio graceful shutdown sequence.
func (c *client) Close() error {
	c.closeOnce.Do(func() {
		_ = c.stdin.Close()
		select {
		case err := <-c.wait:
			if err != nil && !strings.Contains(err.Error(), "signal: killed") {
				c.closeErr = fmt.Errorf("waiting for downstream MCP server: %w", err)
			}
		case <-time.After(2 * time.Second):
			if c.cmd.Process != nil {
				if err := c.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
					c.closeErr = fmt.Errorf("forcing downstream MCP server shutdown: %w", err)
				}
			}
			<-c.wait
			if c.closeErr == nil {
				c.closeErr = errors.New("downstream MCP server required forced shutdown")
			}
		}
	})
	return c.closeErr
}

func (c *client) roundTrip(ctx context.Context, method string, params map[string]any, target any) error {
	c.callMu.Lock()
	defer c.callMu.Unlock()

	id := c.nextID.Add(1)
	requestParams := make(map[string]any, len(params)+1)
	for key, value := range params {
		requestParams[key] = value
	}
	requestParams["_meta"] = map[string]any{
		"io.modelcontextprotocol/protocolVersion":    ProtocolVersion,
		"io.modelcontextprotocol/clientInfo":         map[string]any{"name": "skillex", "version": "dev"},
		"io.modelcontextprotocol/clientCapabilities": map[string]any{},
	}
	request := rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: requestParams}
	encoded, err := json.Marshal(request)
	if err != nil {
		return err
	}
	if len(encoded) > maxMessageBytes {
		return errors.New("downstream MCP request exceeds message limit")
	}
	if _, err := c.stdin.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("writing downstream MCP request: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			if c.cmd.Process != nil {
				if err := c.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
					return fmt.Errorf("%w; stopping downstream MCP server: %v", ctx.Err(), err)
				}
			}
			return ctx.Err()
		case err := <-c.readErr:
			return fmt.Errorf("reading downstream MCP response: %w; stderr=%q", err, c.stderr.String())
		case response := <-c.responses:
			if response.ID != id {
				return fmt.Errorf("downstream MCP response id %d does not match request id %d", response.ID, id)
			}
			if response.Error != nil {
				return fmt.Errorf("downstream MCP error %d: %s", response.Error.Code, bounded(response.Error.Message, 512))
			}
			if err := json.Unmarshal(response.Result, target); err != nil {
				return fmt.Errorf("decoding downstream MCP result: %w", err)
			}
			return nil
		}
	}
}

func (c *client) readLoop(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64<<10), maxMessageBytes)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var envelope struct {
			Method string `json:"method"`
		}
		if err := json.Unmarshal(line, &envelope); err != nil {
			c.readErr <- fmt.Errorf("invalid JSON-RPC message: %w", err)
			return
		}
		if envelope.Method != "" {
			continue
		}
		var response rpcResponse
		if err := json.Unmarshal(line, &response); err != nil {
			c.readErr <- err
			return
		}
		c.responses <- response
	}
	if err := scanner.Err(); err != nil {
		c.readErr <- err
		return
	}
	c.readErr <- io.EOF
}

// ToolResult is the preserved downstream tools/call result envelope.
type ToolResult struct {
	ResultType        string          `json:"resultType"`
	Content           json.RawMessage `json:"content,omitempty"`
	StructuredContent any             `json:"structuredContent,omitempty"`
	IsError           bool            `json:"isError,omitempty"`
}

type toolDefinition struct {
	Name         string          `json:"name"`
	Title        string          `json:"title,omitempty"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
}

type discoverResult struct {
	ResultType        string                     `json:"resultType"`
	SupportedVersions []string                   `json:"supportedVersions"`
	Capabilities      map[string]json.RawMessage `json:"capabilities"`
}

type listToolsResult struct {
	ResultType string           `json:"resultType"`
	Tools      []toolDefinition `json:"tools"`
	NextCursor string           `json:"nextCursor,omitempty"`
	TTLMS      int64            `json:"ttlMs,omitempty"`
	CacheScope string           `json:"cacheScope,omitempty"`
}

type rpcRequest struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      int64          `json:"id"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type limitedBuffer struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	original := len(p)
	remaining := b.limit - b.buf.Len()
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		_, _ = b.buf.Write(p)
	}
	return original, nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func bounded(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

// ConfiguredServers returns stable identities for diagnostics and tests.
func (f *Factory) ConfiguredServers() []string {
	servers := make([]string, 0, len(f.configs))
	for _, config := range f.configs {
		servers = append(servers, config.CanonicalName+"@"+config.Version)
	}
	sort.Strings(servers)
	return servers
}
