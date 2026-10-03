package config

import "testing"

func TestPackRegistryTrustAndPolicyFailClosed(t *testing.T) {
	for _, tc := range []struct {
		name       string
		registries []PackRegistry
		policy     map[string]interface{}
		valid      bool
	}{
		{"default", nil, nil, true},
		{"bundled mirror", []PackRegistry{{Name: "mirror", URL: "https://mirror.example/manifest.json", TrustedRoot: "bundled"}}, nil, true},
		{"custom project trust", []PackRegistry{{Name: "corp", URL: "https://corp.example/manifest.json", TrustedRoot: "./project-root.json"}}, nil, false},
		{"insecure transport", []PackRegistry{{Name: "mirror", URL: "http://mirror.example/manifest.json"}}, nil, false},
		{"allow filtering", []PackRegistry{{Name: "mirror", URL: "https://mirror.example/manifest.json", Allow: map[string]interface{}{"tiers": []string{"core"}}}}, nil, false},
		{"deny filtering", []PackRegistry{{Name: "mirror", URL: "https://mirror.example/manifest.json", Deny: map[string]interface{}{"tiers": []string{"core"}}}}, nil, false},
		{"global policy", nil, map[string]interface{}{"tiers": []string{"core"}}, false},
		{"multiple sources", []PackRegistry{{Name: "one", URL: "https://one.example/manifest.json"}, {Name: "two", URL: "https://two.example/manifest.json"}}, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{Version: SkillsOnlyConfigVersion, Registries: tc.registries, Policy: tc.policy}
			if err := cfg.Validate(); (err == nil) != tc.valid {
				t.Fatalf("Validate()=%v valid=%v", err, tc.valid)
			}
		})
	}
}
