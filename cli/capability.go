package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/atheory-ai/skillex/internal/broker"
	"github.com/atheory-ai/skillex/internal/brokerruntime"
	"github.com/atheory-ai/skillex/internal/capability"
	"github.com/atheory-ai/skillex/internal/config"
	"github.com/atheory-ai/skillex/internal/registry"
	"github.com/spf13/cobra"
)

const maxCLIArgumentsBytes = 1 << 20

func newCapabilityCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "capability", Short: "Describe or invoke a selected MCP capability"}
	cmd.AddCommand(newCapabilityDescribeCmd(), newCapabilityCallCmd())
	return cmd
}

func newCapabilityDescribeCmd() *cobra.Command {
	var ref string
	var maxBytes int
	cmd := &cobra.Command{
		Use: "describe --ref <capability-ref>", Short: "Describe one selected MCP capability without connecting downstream",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if ref == "" {
				return errors.New("--ref is required (select one from skillex query)")
			}
			runtime, closeRegistry, err := openCapabilityRuntime()
			if err != nil {
				return err
			}
			defer closeRegistry()
			selected, err := runtime.Broker.Describe(cmd.Context(), ref, runtimeRequest(runtime))
			if err != nil {
				return err
			}
			encoded, err := capability.MarshalDescription(selected, maxBytes)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(os.Stdout, string(encoded))
			return err
		},
	}
	cmd.Flags().StringVar(&ref, "ref", "", "Capability ref returned by skillex query")
	cmd.Flags().IntVar(&maxBytes, "max-bytes", capability.DefaultDescriptionMaxBytes, "Maximum encoded description bytes (up to 65536)")
	return cmd
}

func newCapabilityCallCmd() *cobra.Command {
	var ref, arguments, inputResponses, requestState string
	cmd := &cobra.Command{
		Use: "call --ref <capability-ref> --arguments <json|->", Short: "Invoke one selected downstream MCP tool",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if ref == "" {
				return errors.New("--ref is required (select one from skillex query)")
			}
			parsed, err := parseCapabilityArguments(arguments)
			if err != nil {
				return err
			}
			var parsedResponses map[string]any
			if inputResponses != "" {
				parsedResponses, err = parseCapabilityArguments(inputResponses)
				if err != nil {
					return fmt.Errorf("invalid MRTR input responses: %w", err)
				}
			}
			runtime, closeRegistry, err := openCapabilityRuntime()
			if err != nil {
				return err
			}
			defer closeRegistry()
			result, err := runtime.Broker.CallWithInput(cmd.Context(), ref, parsed, parsedResponses, requestState, runtimeRequest(runtime))
			if err != nil {
				return err
			}
			encoder := json.NewEncoder(os.Stdout)
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		},
	}
	cmd.Flags().StringVar(&ref, "ref", "", "Capability ref returned by skillex query")
	cmd.Flags().StringVar(&arguments, "arguments", "{}", "JSON object, or - to read a bounded object from stdin")
	cmd.Flags().StringVar(&inputResponses, "input-responses", "", "MRTR response JSON object keyed by input request id")
	cmd.Flags().StringVar(&requestState, "request-state", "", "Opaque MRTR requestState to echo byte-for-byte")
	return cmd
}

func openCapabilityRuntime() (*brokerruntime.Runtime, func(), error) {
	root := repoRoot()
	cfg, err := config.Load(root)
	if err != nil {
		return nil, func() {}, err
	}
	if !cfg.MCPEnabled() {
		return nil, func() {}, broker.ErrMCPDisabled
	}
	reg, err := registry.Open(filepath.Join(root, ".skillex", "index.db"))
	if err != nil {
		return nil, func() {}, err
	}
	runtime, err := brokerruntime.NewDiscovery(root, cfg, reg)
	if err != nil {
		reg.Close()
		return nil, func() {}, err
	}
	return runtime, func() { _ = reg.Close() }, nil
}

func runtimeRequest(runtime *brokerruntime.Runtime) broker.RequestContext {
	return broker.RequestContext{ContextDigest: runtime.ContextDigest, View: runtime.View}
}

func parseCapabilityArguments(value string) (map[string]any, error) {
	var data []byte
	if value == "-" {
		read, err := io.ReadAll(io.LimitReader(os.Stdin, maxCLIArgumentsBytes+1))
		if err != nil {
			return nil, err
		}
		data = read
	} else {
		data = []byte(value)
	}
	if len(data) > maxCLIArgumentsBytes {
		return nil, fmt.Errorf("capability arguments exceed %d bytes", maxCLIArgumentsBytes)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("arguments must be a JSON object: %w", err)
	}
	if parsed == nil {
		parsed = map[string]any{}
	}
	return parsed, nil
}
