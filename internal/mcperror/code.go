// Package mcperror maps internal sentinel errors to a stable, safe broker
// error contract for CLI, MCP, telemetry, and hosted API surfaces.
package mcperror

import (
	"errors"

	"github.com/atheory-ai/skillex/internal/auth"
	"github.com/atheory-ai/skillex/internal/broker"
	"github.com/atheory-ai/skillex/internal/capability"
	"github.com/atheory-ai/skillex/internal/connector/stdio"
	"github.com/atheory-ai/skillex/internal/connector/streamhttp"
	"github.com/atheory-ai/skillex/internal/trust"
)

type Problem struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	Action    string `json:"next_action,omitempty"`
}

func From(err error) Problem {
	problem := Problem{Code: "TOOL_CALL_FAILED", Message: err.Error()}
	switch {
	case errors.Is(err, capability.ErrInvalidReference):
		problem.Code = "CAPABILITY_REF_INVALID"
	case errors.Is(err, capability.ErrDescriptionBudgetInvalid):
		problem.Code, problem.Action = "CAPABILITY_DESCRIPTION_BUDGET_INVALID", "set max_bytes between 1 and 65536"
	case errors.Is(err, capability.ErrDescriptionTooLarge):
		problem.Code, problem.Action = "CAPABILITY_DESCRIPTION_TOO_LARGE", "increase max_bytes up to 65536"
	case errors.Is(err, capability.ErrExpiredReference):
		problem.Code, problem.Action = "CAPABILITY_REF_EXPIRED", "query again to obtain a fresh capability reference"
	case errors.Is(err, broker.ErrContextMismatch), errors.Is(err, broker.ErrViewMismatch):
		problem.Code, problem.Action = "CAPABILITY_NOT_VISIBLE", "query again in the current project context"
	case errors.Is(err, broker.ErrCapabilityChanged), errors.Is(err, stdio.ErrSchemaChanged), errors.Is(err, streamhttp.ErrSchemaChanged):
		problem.Code, problem.Action = "CAPABILITY_SCHEMA_CHANGED", "refresh and query the capability again"
	case errors.Is(err, broker.ErrPolicyDenied), errors.Is(err, trust.ErrProjectUntrusted), errors.Is(err, trust.ErrProfileUntrusted):
		problem.Code = "CAPABILITY_POLICY_DENIED"
	case errors.Is(err, broker.ErrApprovalRequired):
		problem.Code, problem.Action = "CAPABILITY_APPROVAL_REQUIRED", "approve the attributed downstream server and capability"
	case errors.Is(err, trust.ErrCredentialMissing):
		problem.Code, problem.Action = "AUTH_CREDENTIAL_MISSING", "configure the exact credential source mapped by the trusted profile"
	case errors.Is(err, auth.ErrLoginRequired):
		problem.Code, problem.Action = "AUTH_LOGIN_REQUIRED", "run skillex auth login for the selected profile"
	case errors.Is(err, auth.ErrScopeRequired):
		problem.Code, problem.Action = "AUTH_SCOPE_REQUIRED", "authorize the scopes required by the selected capability"
	case errors.Is(err, auth.ErrTokenExchange), errors.Is(err, auth.ErrDiscoveryInvalid), errors.Is(err, auth.ErrClientRegistration):
		problem.Code, problem.Retryable = "AUTH_EXCHANGE_FAILED", true
	case errors.Is(err, trust.ErrServerUntrusted), errors.Is(err, stdio.ErrServerNotConfigured), errors.Is(err, streamhttp.ErrServerNotConfigured):
		problem.Code = "SERVER_UNTRUSTED"
	case errors.Is(err, stdio.ErrProtocolUnsupported):
		problem.Code = "SERVER_PROTOCOL_UNSUPPORTED"
	case errors.Is(err, broker.ErrToolArgumentInvalid):
		problem.Code = "TOOL_ARGUMENT_INVALID"
	case errors.Is(err, stdio.ErrToolResultInvalid), errors.Is(err, streamhttp.ErrToolResultInvalid):
		problem.Code = "TOOL_RESULT_INVALID"
	case errors.Is(err, broker.ErrCapabilityNotReady):
		problem.Code = "CAPABILITY_NOT_VISIBLE"
	}
	return problem
}
