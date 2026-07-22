package acceptance

import (
	"fmt"
	"os"
	"testing"

	"github.com/atheory-ai/skillex/test/helpers"
)

// TestMain keeps direct `go test ./...` runs honest: acceptance tests always
// exercise the current checkout, not a global install or stale root binary.
func TestMain(m *testing.M) {
	if err := helpers.BuildSkilexBinary(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}
