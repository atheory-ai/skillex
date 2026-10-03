package config

import "testing"

func TestInstallValidation(t *testing.T) {
	for _, cfg := range []*InstallConfig{{Strategy: "wat"}, {Strategy: "local-dev-dependency", PackageManager: "yarn"}, {Strategy: "source", Binary: "../skillex"}, {Strategy: "source", Binary: "/tmp/skillex"}, {Strategy: "global", PackageManager: "npm"}} {
		if cfg.Validate() == nil {
			t.Fatalf("accepted %#v", cfg)
		}
	}
	for _, format := range []Format{FormatJSON, FormatYAML} {
		cfg := DefaultConfig()
		cfg.Install = &InstallConfig{Strategy: "local-dev-dependency", PackageManager: "pnpm"}
		data, err := Marshal(cfg, format)
		if err != nil {
			t.Fatal(err)
		}
		var got Config
		if err := decodeConfig(data, format, true, &got); err != nil {
			t.Fatal(err)
		}
		if got.Install.PackageManager != "pnpm" {
			t.Fatal("lost invocation")
		}
	}
}
