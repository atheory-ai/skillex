package verify

import (
	"encoding/json"
	"os"
	"testing"
)

func TestManifestPublishedBundle(t *testing.T) {
	artifact, err := os.ReadFile("testdata/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := os.ReadFile("testdata/manifest.json.bundle")
	if err != nil {
		t.Fatal(err)
	}
	if err := Manifest(artifact, envelope); err != nil {
		t.Fatal(err)
	}
	if err := Manifest(append(artifact, ' '), envelope); err == nil {
		t.Fatal("accepted modified manifest")
	}
	var damaged map[string]any
	if err := json.Unmarshal(envelope, &damaged); err != nil {
		t.Fatal(err)
	}
	damaged["base64Signature"] = "AAAAAAAAAA=="
	data, err := json.Marshal(damaged)
	if err != nil {
		t.Fatal(err)
	}
	if err := Manifest(artifact, data); err == nil {
		t.Fatal("accepted invalid signature")
	}
	for _, field := range []string{"cert", "body", "integratedTime", "logIndex", "logID", "SignedEntryTimestamp"} {
		t.Run(field, func(t *testing.T) {
			var changed map[string]any
			if err := json.Unmarshal(envelope, &changed); err != nil {
				t.Fatal(err)
			}
			rekor := changed["rekorBundle"].(map[string]any)
			payload := rekor["Payload"].(map[string]any)
			switch field {
			case "cert":
				changed[field] = "AAAA"
			case "SignedEntryTimestamp":
				rekor[field] = "AAAA"
			case "body":
				payload[field] = "AAAA"
			case "logID":
				payload[field] = "0000000000000000000000000000000000000000000000000000000000000000"
			default:
				payload[field] = payload[field].(float64) + 1
			}
			data, err := json.Marshal(changed)
			if err != nil {
				t.Fatal(err)
			}
			if err := Manifest(artifact, data); err == nil {
				t.Fatalf("accepted changed %s", field)
			}
		})
	}
}
