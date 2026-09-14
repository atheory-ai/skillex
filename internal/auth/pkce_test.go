package auth

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAuthorizationCodeUsesPKCEResourceStateAndIssuer(t *testing.T) {
	metadata := Metadata{
		Resource: "https://mcp.example", AuthorizationServer: "https://auth.example",
		AuthorizationEndpoint: "https://auth.example/authorize", TokenEndpoint: "https://auth.example/token",
		CodeChallengeMethods: []string{"S256"}, AuthorizationResponseIssuer: true,
	}
	authorizationURL, pending, err := StartAuthorization(metadata, AuthorizationOptions{
		ClientID: "https://client.example/client.json", RedirectURI: "http://127.0.0.1/callback", Scopes: []string{"issues:read"},
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(authorizationURL)
	if parsed.Query().Get("code_challenge_method") != "S256" || parsed.Query().Get("resource") != metadata.Resource || parsed.Query().Get("state") != pending.State {
		t.Fatalf("authorization URL = %s", authorizationURL)
	}
	var form url.Values
	client := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(request.Body)
		form, _ = url.ParseQuery(string(body))
		return response(http.StatusOK, `{"access_token":"access","refresh_token":"refresh","token_type":"Bearer","expires_in":300}`), nil
	})}
	callback := "http://127.0.0.1/callback?code=code&state=" + url.QueryEscape(pending.State) + "&iss=" + url.QueryEscape(metadata.AuthorizationServer)
	token, err := CompleteAuthorization(context.Background(), client, metadata, pending, callback)
	if err != nil {
		t.Fatal(err)
	}
	if token.AuthorizationHeader() != "Bearer access" || form.Get("code_verifier") != pending.Verifier || form.Get("resource") != metadata.Resource {
		t.Fatalf("token/form = %q %#v", token.AuthorizationHeader(), form)
	}
	_, err = CompleteAuthorization(context.Background(), client, metadata, pending, "http://127.0.0.1/callback?code=x&state=wrong&iss=https://auth.example")
	if err != ErrAuthorizationState {
		t.Fatalf("bad state error = %v", err)
	}
}

func TestEncryptedTokenStoreDoesNotPersistPlainTokens(t *testing.T) {
	root := t.TempDir()
	store := TokenStore{KeyPath: filepath.Join(root, "key"), Dir: filepath.Join(root, "tokens")}
	token := Token{value: "plain-access", refresh: "plain-refresh", expiresAt: time.Now().Add(time.Hour), scopes: "read"}
	if err := store.Save("work", token); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "tokens", "work.token"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "plain-access") || strings.Contains(string(data), "plain-refresh") {
		t.Fatalf("token store persisted plaintext: %q", data)
	}
	loaded, err := store.Load("work")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AuthorizationHeader() != "Bearer plain-access" || loaded.refresh != "plain-refresh" {
		t.Fatalf("loaded token = %#v", loaded)
	}
	if err := store.SavePending("work", PendingAuthorization{State: "state", Verifier: "verifier", ExpiresAt: time.Now().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	pending, err := store.LoadPending("work")
	if err != nil || pending.Verifier != "verifier" {
		t.Fatalf("pending = %#v, %v", pending, err)
	}
}
