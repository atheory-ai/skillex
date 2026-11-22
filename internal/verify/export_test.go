package verify

import "testing"

// SignFixture signs a test catalog and returns its public test root.
// Neither this signer nor the root override is compiled into Skillex.
func SignFixture(t testing.TB, manifest []byte) ([]byte, []byte) {
	t.Helper()
	root, bundle := syntheticSignedManifest(t, manifest, ManifestIdentity, "https://token.actions.githubusercontent.com")
	data, err := root.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return data, bundle
}

func UseFixtureRoot(t testing.TB, data []byte) {
	t.Helper()
	previous := trustedRootJSON
	previousRegistries := manifestRegistries
	t.Cleanup(func() { trustedRootJSON, manifestRegistries = previous, previousRegistries })
	// These synthetic catalogs exercise the resolver, updates, and withdrawals
	// without publishing fixture archives through the real release workflow.
	manifestRegistries = map[string]registryPolicy{PackRegistry: {}, "fixture": {}}
	trustedRootJSON = data
}
