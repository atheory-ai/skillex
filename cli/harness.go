package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/atheory-ai/skillex/internal/config"
)

func harnessConfigPath(root, harness string) (string, error) {
	switch harness {
	case "cursor":
		return filepath.Join(root, ".cursor", "mcp.json"), nil
	case "claude-code":
		return filepath.Join(root, ".mcp.json"), nil
	case "windsurf":
		return filepath.Join(root, ".windsurf", "mcp.json"), nil
	default:
		return "", fmt.Errorf("unknown harness %q (supported: cursor, claude-code, windsurf)", harness)
	}
}

func detectHarness(root string) string {
	var detected []string
	for _, h := range []struct {
		name  string
		files []string
	}{
		{"cursor", []string{".cursor"}},
		{"claude-code", []string{".claude", "CLAUDE.md", ".mcp.json"}},
		{"windsurf", []string{".windsurf", ".windsurfrules"}},
	} {
		for _, file := range h.files {
			if _, err := os.Stat(filepath.Join(root, file)); err == nil {
				detected = append(detected, h.name)
				break
			}
		}
	}
	if len(detected) == 1 {
		return detected[0]
	}
	return "none"
}

func chooseHarness(in io.Reader, out io.Writer, detected string) (string, error) {
	fmt.Fprintf(out, "Configure harness-managed Skillex MCP [none/cursor/claude-code/windsurf] (%s): ", detected)
	answer, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		answer = detected
	}
	if answer == "none" {
		return "", nil
	}
	if _, err := harnessConfigPath(".", answer); err != nil {
		return "", err
	}
	return answer, nil
}

// configureMCP merges Skillex into user-owned harness configuration without
// dropping unrelated servers, root settings, or extra Skillex server settings.
func configureMCP(root, harness string, overwrite bool) error {
	path, err := harnessConfigPath(root, harness)
	if err != nil {
		return err
	}
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	command, prefix := cfg.Install.Command()
	args := append(prefix, "mcp")
	commandJSON, err := json.Marshal(command)
	if err != nil {
		return err
	}
	argsJSON, err := json.Marshal(args)
	if err != nil {
		return err
	}
	document := map[string]json.RawMessage{}
	data, err := os.ReadFile(path)
	exists := err == nil
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading MCP config: %w", err)
	}
	if exists {
		if err := json.Unmarshal(data, &document); err != nil {
			return fmt.Errorf("invalid MCP config %s: %w", path, err)
		}
		if document == nil {
			return fmt.Errorf("MCP config %s must be a JSON object", path)
		}
	}
	servers := map[string]json.RawMessage{}
	if raw, ok := document["mcpServers"]; ok {
		if err := json.Unmarshal(raw, &servers); err != nil || servers == nil {
			return fmt.Errorf("MCP config %s: mcpServers must be a JSON object", path)
		}
	}
	server := map[string]json.RawMessage{}
	if raw, ok := servers["skillex"]; ok {
		if err := json.Unmarshal(raw, &server); err != nil || server == nil {
			return fmt.Errorf("MCP config %s: skillex must be a JSON object", path)
		}
		var existingCommand string
		var existingArgs []string
		commandErr := json.Unmarshal(server["command"], &existingCommand)
		argsErr := json.Unmarshal(server["args"], &existingArgs)
		if commandErr == nil && argsErr == nil && existingCommand == command && reflect.DeepEqual(existingArgs, args) {
			return nil
		}
		if !overwrite {
			return fmt.Errorf("%s already contains a different skillex server; preserved existing config (use --overwrite-mcp to update only its command and args)", path)
		}
	}
	server["command"], server["args"] = commandJSON, argsJSON
	servers["skillex"], err = json.Marshal(server)
	if err != nil {
		return err
	}
	document["mcpServers"], err = json.Marshal(servers)
	if err != nil {
		return err
	}
	updated, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if exists {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		mode = info.Mode().Perm()
	}
	// Stage the complete document in the same directory before replacing it.
	temp, err := os.CreateTemp(filepath.Dir(path), ".skillex-mcp-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if err := temp.Chmod(mode); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(append(updated, '\n')); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}
