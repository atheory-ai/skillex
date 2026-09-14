// Package jsonschema validates MCP inputs and outputs against JSON Schema
// 2020-12 without allowing schema compilation to fetch external resources.
package jsonschema

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	validator "github.com/santhosh-tekuri/jsonschema/v6"
)

var errExternalResourceLoading = errors.New("external JSON Schema resource loading is disabled")

// Validate compiles a bounded in-memory schema and validates a JSON-compatible value.
func Validate(schema []byte, value any) error {
	if len(bytes.TrimSpace(schema)) == 0 || bytes.Equal(bytes.TrimSpace(schema), []byte("null")) {
		return nil
	}
	document, err := validator.UnmarshalJSON(bytes.NewReader(schema))
	if err != nil {
		return fmt.Errorf("invalid JSON Schema: %w", err)
	}
	compiler := validator.NewCompiler()
	compiler.DefaultDraft(validator.Draft2020)
	compiler.UseLoader(rejectExternalLoader{})
	const resource = "https://skillex.invalid/mcp-tool-schema.json"
	if err := compiler.AddResource(resource, document); err != nil {
		return err
	}
	compiled, err := compiler.Compile(resource)
	if err != nil {
		if errors.Is(err, errExternalResourceLoading) || strings.Contains(err.Error(), errExternalResourceLoading.Error()) {
			return fmt.Errorf("invalid JSON Schema: %w", errExternalResourceLoading)
		}
		return fmt.Errorf("invalid JSON Schema: %w", err)
	}
	return compiled.Validate(value)
}

type rejectExternalLoader struct{}

func (rejectExternalLoader) Load(url string) (any, error) {
	return nil, fmt.Errorf("%w: %s", errExternalResourceLoading, safeURL(url))
}

func safeURL(value string) string {
	if before, _, ok := strings.Cut(value, "?"); ok {
		value = before
	}
	if before, _, ok := strings.Cut(value, "#"); ok {
		value = before
	}
	return value
}
