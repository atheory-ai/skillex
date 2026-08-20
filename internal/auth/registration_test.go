package auth

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestClientMetadataDocumentRequiresSelfIdentifyingClientAndRedirect(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.String() != "https://client.example/skillex.json" {
			t.Fatalf("unexpected client metadata URL %s", request.URL)
		}
		return response(http.StatusOK, `{"client_id":"https://client.example/skillex.json","redirect_uris":["http://127.0.0.1/callback"],"grant_types":["authorization_code"],"response_types":["code"],"token_endpoint_auth_method":"none"}`), nil
	})}
	registration, err := ValidateClientMetadataDocument(context.Background(), client, "https://client.example/skillex.json", "http://127.0.0.1/callback")
	if err != nil {
		t.Fatal(err)
	}
	if registration.ClientID != "https://client.example/skillex.json" || registration.AuthMethod != "none" {
		t.Fatalf("registration = %#v", registration)
	}
}

func TestDynamicRegistrationIsExplicitAndBounded(t *testing.T) {
	client := &http.Client{Transport: roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(request.Body)
		if !strings.Contains(string(body), `"redirect_uris":["http://127.0.0.1/callback"]`) {
			t.Fatalf("registration body = %s", body)
		}
		return response(http.StatusCreated, `{"client_id":"generated-client","token_endpoint_auth_method":"none"}`), nil
	})}
	registration, err := RegisterDynamicClient(context.Background(), client, DynamicRegistrationRequest{
		Metadata:   Metadata{DynamicRegistrationEndpoint: "https://auth.example/register"},
		ClientName: "Skillex", RedirectURI: "http://127.0.0.1/callback",
	})
	if err != nil {
		t.Fatal(err)
	}
	if registration.ClientID != "generated-client" || registration.AuthMethod != "none" {
		t.Fatalf("registration = %#v", registration)
	}
}
