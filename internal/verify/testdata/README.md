# Signature verification fixtures

`manifest.json` and `manifest.json.bundle` are the public registry's real signed
manifest, verified against the engine's bundled public Sigstore root.

The `compatible-*` files are synthetic, engine-compatible fixtures. Their
certificates, artifact signatures, embedded certificate transparency SCTs, and
Rekor signed-entry timestamps are real cryptographic evidence. The adjacent
`compatible-trusted-root.json` is public test trust material and is passed only
through private test adapters. Production verification rejects these fixtures.
The revoked variant is independently signed and exercises authenticated
revocation rejection before archive download.

No private keys are retained. Regenerate the synthetic public fixtures with:

```sh
SKILLEX_REGENERATE_VERIFY_FIXTURES=1 go test ./internal/verify -run '^TestRegenerateCompatibleManifestFixture$' -count=1
```

The regeneration code is compiled only in Go test builds. There is no environment
variable or command-line option to change production trust. Fixtures are verified
at their authenticated signing time, so their short-lived certificates do not
require periodic regeneration.
