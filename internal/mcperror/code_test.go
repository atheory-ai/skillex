package mcperror

import (
	"fmt"
	"testing"

	"github.com/atheory-ai/skillex/internal/auth"
	"github.com/atheory-ai/skillex/internal/capability"
)

func TestStableCodesSurviveWrappedErrors(t *testing.T) {
	tests := []struct {
		err  error
		code string
	}{
		{fmt.Errorf("verify: %w", capability.ErrExpiredReference), "CAPABILITY_REF_EXPIRED"},
		{fmt.Errorf("describe: %w", capability.ErrDescriptionTooLarge), "CAPABILITY_DESCRIPTION_TOO_LARGE"},
		{fmt.Errorf("auth: %w", auth.ErrLoginRequired), "AUTH_LOGIN_REQUIRED"},
		{fmt.Errorf("scope: %w", auth.ErrScopeRequired), "AUTH_SCOPE_REQUIRED"},
	}
	for _, test := range tests {
		if got := From(test.err); got.Code != test.code {
			t.Fatalf("From(%v) = %#v", test.err, got)
		}
	}
}
