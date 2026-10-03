package packregistry

import (
	"context"
	"net/http"
)

// These adapters exist only in test builds. Production public entry points
// always use the immutable embedded Sigstore root and normal HTTPS downloader.
func PrepareForTest(ctx context.Context, address, name string, verifier func([]byte, []byte) error, fetch func(context.Context, string, int64) ([]byte, error)) (*Prepared, error) {
	return prepareWithVerifier(ctx, address, name, verifier, fetch)
}
func ValidateDirectoryForTest(dir string, verifier func([]byte, []byte) error) (Lock, error) {
	return validateDirectoryWithVerifier(dir, verifier)
}
func ListForTest(root string, verifier func([]byte, []byte) error) ([]Lock, error) {
	return listWithVerifier(root, verifier)
}
func DownloadForTest(ctx context.Context, address string, limit int64, client *http.Client) ([]byte, error) {
	return downloadWithClient(ctx, address, limit, client)
}
