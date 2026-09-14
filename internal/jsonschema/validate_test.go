package jsonschema

import (
	"strings"
	"testing"
)

func TestValidateRejectsExternalSchemaResources(t *testing.T) {
	err := Validate([]byte(`{"$ref":"https://attacker.example/schema.json?secret=value#payload"}`), map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "external JSON Schema resource loading is disabled") {
		t.Fatalf("external schema error = %v", err)
	}
	if strings.Contains(err.Error(), "secret=value") || strings.Contains(err.Error(), "payload") {
		t.Fatalf("external schema error leaked URL details: %v", err)
	}
}

func BenchmarkValidateCapabilityArguments(b *testing.B) {
	schema := []byte(`{
		"$schema":"https://json-schema.org/draft/2020-12/schema",
		"type":"object",
		"properties":{
			"title":{"type":"string","minLength":1},
			"labels":{"type":"array","items":{"type":"string"},"maxItems":20}
		},
		"required":["title"],
		"additionalProperties":false
	}`)
	arguments := map[string]any{"title": "release", "labels": []any{"mcp", "v0.9.0"}}
	b.ReportAllocs()
	for range b.N {
		if err := Validate(schema, arguments); err != nil {
			b.Fatal(err)
		}
	}
}
