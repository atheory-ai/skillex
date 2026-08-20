// Command mcpserver is a strict, deterministic MCP 2026-07-28 stdio server for
// acceptance tests. It is not shipped in Skillex release artifacts.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
)

const protocolVersion = "2026-07-28"

type fixture struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Tools   []tool `json:"tools"`
}

type tool struct {
	Name         string          `json:"name"`
	Title        string          `json:"title,omitempty"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"inputSchema"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type response struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Result  any    `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type event struct {
	Method string `json:"method"`
	Tool   string `json:"tool,omitempty"`
}

func main() {
	fixturePath := flag.String("fixture", "", "path to a fake MCP server fixture")
	eventPath := flag.String("events", "", "path for JSONL protocol events")
	flag.Parse()
	if *fixturePath == "" || *eventPath == "" {
		fatal(errors.New("--fixture and --events are required"))
	}
	data, err := os.ReadFile(*fixturePath)
	if err != nil {
		fatal(err)
	}
	var config fixture
	if err := json.Unmarshal(data, &config); err != nil {
		fatal(err)
	}
	if config.Name == "" || config.Version == "" {
		fatal(errors.New("fixture name and version are required"))
	}
	tools := make(map[string]tool, len(config.Tools))
	for _, candidate := range config.Tools {
		if candidate.Name == "" {
			fatal(errors.New("fixture tool name is required"))
		}
		tools[candidate.Name] = candidate
	}

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64<<10), 4<<20)
	encoder := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var req request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			_ = encoder.Encode(errorResponse(0, -32700, "parse error"))
			continue
		}
		if err := validateMeta(req.Params); err != nil {
			_ = appendEvent(*eventPath, event{Method: req.Method})
			_ = encoder.Encode(errorResponse(req.ID, -32600, err.Error()))
			continue
		}
		if err := appendEvent(*eventPath, event{Method: req.Method, Tool: requestedTool(req)}); err != nil {
			fatal(err)
		}

		switch req.Method {
		case "server/discover":
			_ = encoder.Encode(response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
				"resultType":        "complete",
				"supportedVersions": []string{protocolVersion},
				"capabilities":      map[string]any{"tools": map[string]any{}},
				"_meta": map[string]any{
					"io.modelcontextprotocol/serverInfo": map[string]any{"name": config.Name, "version": config.Version},
				},
				"ttlMs":      300000,
				"cacheScope": "public",
			}})
		case "tools/list":
			ordered := append([]tool(nil), config.Tools...)
			sort.Slice(ordered, func(i, j int) bool { return ordered[i].Name < ordered[j].Name })
			_ = encoder.Encode(response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
				"resultType": "complete",
				"tools":      ordered,
				"ttlMs":      300000,
				"cacheScope": "public",
			}})
		case "tools/call":
			var params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			if err := json.Unmarshal(req.Params, &params); err != nil {
				_ = encoder.Encode(errorResponse(req.ID, -32602, "invalid tool parameters"))
				continue
			}
			if _, ok := tools[params.Name]; !ok {
				_ = encoder.Encode(errorResponse(req.ID, -32602, "unknown tool"))
				continue
			}
			structured := map[string]any{
				"server":        config.Name,
				"tool":          params.Name,
				"arguments":     params.Arguments,
				"secretVisible": os.Getenv("SKILLEX_TEST_SECRET") != "",
			}
			encoded, _ := json.Marshal(structured)
			_ = encoder.Encode(response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
				"resultType":        "complete",
				"content":           []map[string]any{{"type": "text", "text": string(encoded)}},
				"structuredContent": structured,
				"isError":           false,
			}})
		default:
			_ = encoder.Encode(errorResponse(req.ID, -32601, "method not found"))
		}
	}
	if err := scanner.Err(); err != nil {
		fatal(err)
	}
}

func validateMeta(raw json.RawMessage) error {
	var params struct {
		Meta map[string]json.RawMessage `json:"_meta"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return errors.New("params must be an object")
	}
	var version string
	if err := json.Unmarshal(params.Meta["io.modelcontextprotocol/protocolVersion"], &version); err != nil || version != protocolVersion {
		return errors.New("missing or unsupported protocol version metadata")
	}
	if len(params.Meta["io.modelcontextprotocol/clientCapabilities"]) == 0 {
		return errors.New("missing client capabilities metadata")
	}
	return nil
}

func requestedTool(req request) string {
	if req.Method != "tools/call" {
		return ""
	}
	var params struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(req.Params, &params)
	return params.Name
}

func appendEvent(path string, value event) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	return json.NewEncoder(file).Encode(value)
}

func errorResponse(id int64, code int, message string) response {
	value := response{JSONRPC: "2.0", ID: id}
	value.Error = &struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}{Code: code, Message: message}
	return value
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
