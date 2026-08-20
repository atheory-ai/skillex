package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

var ErrClientRegistration = errors.New("OAuth client registration is invalid")

// ClientRegistration is the minimum registration material required by the
// supported OAuth flows. Secrets remain encrypted when persisted by TokenStore.
type ClientRegistration struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
	AuthMethod   string `json:"token_endpoint_auth_method,omitempty"`
}

// ValidateClientMetadataDocument validates a URL client_id as a CIMD. The
// document is fetched only during explicit login/token acquisition, never query.
func ValidateClientMetadataDocument(ctx context.Context, client *http.Client, clientID, redirectURI string) (ClientRegistration, error) {
	if err := validateHTTPSURL(clientID); err != nil {
		return ClientRegistration{}, ErrClientRegistration
	}
	var document struct {
		ClientID                string   `json:"client_id"`
		RedirectURIs            []string `json:"redirect_uris"`
		GrantTypes              []string `json:"grant_types"`
		ResponseTypes           []string `json:"response_types"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	}
	if err := getJSON(ctx, secureClient(client), clientID, &document); err != nil {
		return ClientRegistration{}, err
	}
	if document.ClientID != clientID {
		return ClientRegistration{}, ErrClientRegistration
	}
	if redirectURI != "" && !contains(document.RedirectURIs, redirectURI) {
		return ClientRegistration{}, ErrClientRegistration
	}
	if redirectURI != "" && len(document.GrantTypes) > 0 && !contains(document.GrantTypes, "authorization_code") {
		return ClientRegistration{}, ErrClientRegistration
	}
	if redirectURI != "" && len(document.ResponseTypes) > 0 && !contains(document.ResponseTypes, "code") {
		return ClientRegistration{}, ErrClientRegistration
	}
	method := document.TokenEndpointAuthMethod
	if method == "" {
		method = "none"
	}
	return ClientRegistration{ClientID: clientID, AuthMethod: method}, nil
}

type DynamicRegistrationRequest struct {
	Metadata    Metadata
	ClientName  string
	RedirectURI string
	Scopes      []string
}

// RegisterDynamicClient implements explicit compatibility DCR. Callers must
// opt in; it is never selected when CIMD is available.
func RegisterDynamicClient(ctx context.Context, client *http.Client, request DynamicRegistrationRequest) (ClientRegistration, error) {
	if request.Metadata.DynamicRegistrationEndpoint == "" {
		return ClientRegistration{}, ErrClientRegistration
	}
	if err := validateHTTPSURL(request.Metadata.DynamicRegistrationEndpoint); err != nil {
		return ClientRegistration{}, err
	}
	payload := map[string]any{
		"client_name":                request.ClientName,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	}
	if request.RedirectURI != "" {
		payload["redirect_uris"] = []string{request.RedirectURI}
	}
	if len(request.Scopes) > 0 {
		payload["scope"] = strings.Join(request.Scopes, " ")
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return ClientRegistration{}, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, request.Metadata.DynamicRegistrationEndpoint, bytes.NewReader(encoded))
	if err != nil {
		return ClientRegistration{}, err
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")
	response, err := secureClient(client).Do(httpRequest)
	if err != nil {
		return ClientRegistration{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusOK {
		return ClientRegistration{}, ErrClientRegistration
	}
	var registered ClientRegistration
	if err := decodeBounded(response.Body, &registered); err != nil {
		return ClientRegistration{}, ErrClientRegistration
	}
	if registered.ClientID == "" {
		return ClientRegistration{}, ErrClientRegistration
	}
	if registered.AuthMethod == "" {
		registered.AuthMethod = "none"
	}
	if registered.AuthMethod != "none" {
		return ClientRegistration{}, errors.New("dynamic authorization-code clients must use token_endpoint_auth_method none")
	}
	return registered, nil
}

func IsURLClientID(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme != "" && parsed.Host != ""
}
