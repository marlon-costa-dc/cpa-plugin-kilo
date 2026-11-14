package kilo

import (
	"net/http"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func TestCatalogUsesAccountScopeAndDoesNotFallBack(t *testing.T) {
	status := http.StatusOK
	var requests int
	transport := fixtureTransport(func(req pluginapi.HTTPRequest) pluginapi.HTTPResponse {
		requests++
		if req.URL != gatewayBase+"/organizations/team-one/models" || req.Headers.Get(organizationHeader) != "team-one" || req.Headers.Get("Authorization") != "Bearer fixture-token" {
			t.Fatal("catalog request did not use the authorized organization")
		}
		return pluginapi.HTTPResponse{StatusCode: status, Body: []byte(`{"data":[{"id":"provider/model","name":"Discovered model","context_length":64000,"top_provider":{"max_completion_tokens":4096},"supported_parameters":["tools"],"architecture":{"input_modalities":["text","image"]}}]}`)}
	})
	input := pluginapi.AuthModelRequest{AuthID: "fixture", AuthProvider: providerID, HTTPClient: transport,
		Metadata: map[string]any{"access_token": "fixture-token", "organization_id": "team-one"}}
	result, err := ModelsForAuth(t.Context(), input)
	if err != nil || len(result.Models) != 1 || result.Models[0].ID != "provider/model" || result.Models[0].MaxCompletionTokens != 4096 {
		t.Fatalf("catalog did not carry the live gateway model: %v", err)
	}
	status = http.StatusUnauthorized
	result, err = ModelsForAuth(t.Context(), input)
	if err == nil || len(result.Models) != 0 || requests != 2 {
		t.Fatal("an unauthorized account must not return cached or anonymous models")
	}
}
