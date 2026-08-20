package brokerruntime

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/atheory-ai/skillex/internal/broker"
	"github.com/atheory-ai/skillex/internal/config"
	"github.com/atheory-ai/skillex/internal/registry"
)

func TestNewDiscoveryDoesNotCreateKeyForSkillsOnlyConfig(t *testing.T) {
	root := t.TempDir()
	reg, err := registry.Open(filepath.Join(root, ".skillex", "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer reg.Close()
	created, err := NewDiscovery(root, config.DefaultConfig(), reg)
	if !errors.Is(err, broker.ErrMCPDisabled) || created != nil {
		t.Fatalf("NewDiscovery(skills-only) = %#v, %v", created, err)
	}
}
