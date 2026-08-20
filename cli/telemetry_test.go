package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTelemetrySummaryAggregatesPrivacySafeDimensions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	data := []byte("{\"operation\":\"call\",\"server\":\"io.example/issues\",\"capability\":\"issues.create\",\"outcome\":\"success\"}\n" +
		"{\"operation\":\"call\",\"server\":\"io.example/issues\",\"capability\":\"issues.create\",\"outcome\":\"policy-denied\"}\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	summary, err := readTelemetrySummary(path)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Events != 2 || summary.ByOperation["call"] != 2 || summary.ByOutcome["success"] != 1 || summary.ByCapability["io.example/issues/issues.create"] != 2 {
		t.Fatalf("summary = %#v", summary)
	}
}
