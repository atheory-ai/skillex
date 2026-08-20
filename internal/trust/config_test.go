package trust

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/atheory-ai/skillex/internal/capability"
)

func TestStdioConfigResolvesOnlyMappedEnvironmentKey(t *testing.T) {
	root := t.TempDir()
	t.Setenv("MAPPED_TOKEN", "mapped-value")
	t.Setenv("UNMAPPED_SECRET", "must-not-be-inherited")
	cfg := testConfig(root, []CredentialSource{{Env: &EnvSource{Key: "MAPPED_TOKEN"}}})
	selected := testCapability("profile")

	got, err := cfg.StdioConfig(selected, root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Environment, []string{"DOWNSTREAM_TOKEN=mapped-value"}) {
		t.Fatalf("environment = %#v", got.Environment)
	}
	for _, entry := range got.Environment {
		if strings.Contains(entry, "UNMAPPED_SECRET") || strings.Contains(entry, "must-not-be-inherited") {
			t.Fatalf("unmapped secret leaked: %q", entry)
		}
	}
}

func TestStdioConfigReadsExactDotenvKeyAndConfinesProjectTemplate(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".env.mcp"), []byte("OTHER=hidden\nMAPPED=selected\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(root, []CredentialSource{{Dotenv: &DotenvSource{Path: "${projectRoot}/.env.mcp", Key: "MAPPED"}}})
	got, err := cfg.StdioConfig(testCapability("profile"), root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Environment, []string{"DOWNSTREAM_TOKEN=selected"}) {
		t.Fatalf("environment = %#v", got.Environment)
	}

	outside := filepath.Join(t.TempDir(), "outside.env")
	if err := os.WriteFile(outside, []byte("MAPPED=escaped\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape.env")); err != nil {
		t.Fatal(err)
	}
	cfg.CredentialProfiles[0].Credentials[0].Sources[0].Dotenv.Path = "${projectRoot}/escape.env"
	_, err = cfg.StdioConfig(testCapability("profile"), root)
	if err == nil || !strings.Contains(err.Error(), "outside project root") {
		t.Fatalf("symlink escape error = %v", err)
	}
}

func TestReadinessDistinguishesMissingCredentialAndPolicy(t *testing.T) {
	root := t.TempDir()
	cfg := testConfig(root, []CredentialSource{{Env: &EnvSource{Key: "ABSENT_TEST_TOKEN"}}})
	if got := cfg.Ready(testCapability("profile"), root); got != capability.AvailabilityCredentialMissing {
		t.Fatalf("missing credential availability = %s", got)
	}
	if got := cfg.Ready(testCapability("other-profile"), root); got != capability.AvailabilityPolicyDenied {
		t.Fatalf("untrusted profile availability = %s", got)
	}
	if got := cfg.Ready(testCapability("profile"), t.TempDir()); got != capability.AvailabilitySetupRequired {
		t.Fatalf("unapproved project availability = %s", got)
	}
	_, err := cfg.StdioConfig(testCapability("profile"), t.TempDir())
	if !errors.Is(err, ErrProjectUntrusted) {
		t.Fatalf("unapproved project error = %v", err)
	}
}

func testConfig(root string, sources []CredentialSource) *Config {
	return &Config{
		Version: ConfigVersion,
		Servers: []Server{{
			Server: "io.example/service", Version: "1.0.0", AllowedProjects: []string{root},
			AuthProfiles: []string{"profile"}, Stdio: &StdioConfig{Command: "/bin/echo"},
		}},
		CredentialProfiles: []CredentialProfile{{
			Name: "profile", Service: "io.example/service",
			Credentials: []Credential{{
				Slot: "token", Sources: sources, Inject: Injection{StdioEnv: "DOWNSTREAM_TOKEN"},
			}},
		}},
	}
}

func testCapability(profile string) capability.Capability {
	return capability.Capability{
		Server: capability.ServerVersion{Identity: capability.ServerIdentity{CanonicalName: "io.example/service"}, Version: "1.0.0"},
		Kind:   capability.CapabilityTool, Name: "tool", AuthProfile: profile,
	}
}
