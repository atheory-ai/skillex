package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	ErrLoginRequired       = errors.New("interactive OAuth login is required")
	ErrAuthorizationState  = errors.New("OAuth authorization response state is invalid")
	ErrAuthorizationIssuer = errors.New("OAuth authorization response issuer is invalid")
)

type AuthorizationOptions struct {
	ClientID    string
	RedirectURI string
	Scopes      []string
}

type PendingAuthorization struct {
	State       string    `json:"state"`
	Verifier    string    `json:"verifier"`
	Issuer      string    `json:"issuer"`
	Resource    string    `json:"resource"`
	ClientID    string    `json:"client_id"`
	RedirectURI string    `json:"redirect_uri"`
	Scopes      []string  `json:"scopes,omitempty"`
	ExpiresAt   time.Time `json:"expires_at"`
}

func StartAuthorization(metadata Metadata, options AuthorizationOptions) (string, PendingAuthorization, error) {
	if !contains(metadata.CodeChallengeMethods, "S256") || metadata.AuthorizationEndpoint == "" {
		return "", PendingAuthorization{}, errors.New("authorization server does not support required PKCE S256 flow")
	}
	if err := validateHTTPSURL(metadata.AuthorizationEndpoint); err != nil {
		return "", PendingAuthorization{}, err
	}
	if options.ClientID == "" || options.RedirectURI == "" {
		return "", PendingAuthorization{}, errors.New("OAuth client ID and redirect URI are required")
	}
	verifier, err := randomURLToken(32)
	if err != nil {
		return "", PendingAuthorization{}, err
	}
	state, err := randomURLToken(24)
	if err != nil {
		return "", PendingAuthorization{}, err
	}
	digest := sha256.Sum256([]byte(verifier))
	endpoint, err := url.Parse(metadata.AuthorizationEndpoint)
	if err != nil {
		return "", PendingAuthorization{}, err
	}
	query := endpoint.Query()
	query.Set("response_type", "code")
	query.Set("client_id", options.ClientID)
	query.Set("redirect_uri", options.RedirectURI)
	query.Set("code_challenge", base64.RawURLEncoding.EncodeToString(digest[:]))
	query.Set("code_challenge_method", "S256")
	query.Set("state", state)
	query.Set("resource", metadata.Resource)
	if len(options.Scopes) > 0 {
		query.Set("scope", strings.Join(options.Scopes, " "))
	}
	endpoint.RawQuery = query.Encode()
	pending := PendingAuthorization{
		State: state, Verifier: verifier, Issuer: metadata.AuthorizationServer,
		Resource: metadata.Resource, ClientID: options.ClientID, RedirectURI: options.RedirectURI,
		Scopes: append([]string(nil), options.Scopes...), ExpiresAt: time.Now().Add(10 * time.Minute),
	}
	return endpoint.String(), pending, nil
}

func CompleteAuthorization(ctx context.Context, client *http.Client, metadata Metadata, pending PendingAuthorization, callbackURL string) (Token, error) {
	if time.Now().After(pending.ExpiresAt) {
		return Token{}, ErrAuthorizationState
	}
	callback, err := url.Parse(callbackURL)
	if err != nil {
		return Token{}, ErrAuthorizationState
	}
	query := callback.Query()
	if query.Get("state") != pending.State || query.Get("code") == "" {
		return Token{}, ErrAuthorizationState
	}
	issuer := query.Get("iss")
	if issuer != "" && issuer != pending.Issuer || metadata.AuthorizationResponseIssuer && issuer != pending.Issuer {
		return Token{}, ErrAuthorizationIssuer
	}
	values := url.Values{
		"grant_type": {"authorization_code"}, "code": {query.Get("code")},
		"client_id": {pending.ClientID}, "redirect_uri": {pending.RedirectURI},
		"code_verifier": {pending.Verifier}, "resource": {pending.Resource},
	}
	return tokenRequest(ctx, client, metadata.TokenEndpoint, values, nil)
}

func Refresh(ctx context.Context, client *http.Client, metadata Metadata, clientID string, token Token) (Token, error) {
	if token.refresh == "" {
		return Token{}, ErrLoginRequired
	}
	values := url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {token.refresh},
		"client_id": {clientID}, "resource": {metadata.Resource},
	}
	refreshed, err := tokenRequest(ctx, client, metadata.TokenEndpoint, values, nil)
	if err != nil {
		return Token{}, err
	}
	if refreshed.refresh == "" {
		refreshed.refresh = token.refresh
	}
	return refreshed, nil
}

func randomURLToken(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}
