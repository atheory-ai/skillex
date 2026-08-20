// Package telemetry records privacy-safe MCP broker usage locally when a user
// explicitly opts in through trusted configuration.
package telemetry

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/atheory-ai/skillex/internal/broker"
)

type Event struct {
	Timestamp       time.Time `json:"timestamp"`
	Operation       string    `json:"operation"`
	Server          string    `json:"server,omitempty"`
	Version         string    `json:"version,omitempty"`
	Kind            string    `json:"kind,omitempty"`
	Capability      string    `json:"capability,omitempty"`
	Availability    string    `json:"availability,omitempty"`
	Outcome         string    `json:"outcome"`
	DurationMS      int64     `json:"duration_ms"`
	TenantPartition string    `json:"tenant_partition,omitempty"`
	PrincipalKind   string    `json:"principal_kind,omitempty"`
}

type Local struct {
	path string
	mu   sync.Mutex
}

func NewLocal(path string) *Local { return &Local{path: path} }

func (l *Local) Record(_ context.Context, usage broker.UsageEvent) {
	if l == nil || l.path == "" {
		return
	}
	event := Event{
		Timestamp: time.Now().UTC(), Operation: usage.Operation, Server: usage.Server,
		Version: usage.Version, Kind: string(usage.Kind), Capability: usage.Capability,
		Availability: string(usage.Availability), Outcome: usage.Outcome,
		DurationMS:      usage.Duration.Milliseconds(),
		TenantPartition: usage.TenantPartition, PrincipalKind: usage.PrincipalKind,
	}
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return
	}
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		return
	}
	_ = file.Close()
}
