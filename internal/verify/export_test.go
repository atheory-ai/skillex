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
	t.Cleanup(func() { trustedRootJSON = previous })
	trustedRootJSON = data
}
