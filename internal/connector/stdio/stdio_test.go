package stdio

import (
	"context"
	"errors"
	"testing"

	"github.com/atheory-ai/skillex/internal/capability"
)

func TestNewFactoryRequiresAbsoluteCommand(t *testing.T) {
	_, err := NewFactory([]ServerConfig{{CanonicalName: "io.example/test", Version: "1.0.0", Command: "fake-mcp"}})
	if err == nil {
		t.Fatal("NewFactory accepted a PATH-resolved command")
	}
}

func TestFactoryRejectsUnconfiguredServerBeforeStartingProcess(t *testing.T) {
	factory, err := NewFactory(nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = factory.Open(context.Background(), capability.Capability{
		Server: capability.ServerVersion{Identity: capability.ServerIdentity{CanonicalName: "io.example/missing"}, Version: "1.0.0"},
	})
	if !errors.Is(err, ErrServerNotConfigured) {
		t.Fatalf("Open() error = %v, want %v", err, ErrServerNotConfigured)
	}
}
