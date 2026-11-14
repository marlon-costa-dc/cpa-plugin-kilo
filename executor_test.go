package kilo

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func TestChatPreservesToolsAccountAndUpstreamStatus(t *testing.T) {
	status := http.StatusOK
	request := pluginapi.ExecutorRequest{
		AuthProvider: providerID, AuthID: "fixture", Model: "selected/model",
		AuthMetadata: map[string]any{"access_token": "fixture-token", "organization_id": "team"},
		Payload:      []byte(`{"model":"untrusted/override","tools":[{"type":"function","function":{"name":"lookup"}}],"messages":[{"role":"user","content":"lookup"}]}`),
	}
	request.HTTPClient = fixtureTransport(func(out pluginapi.HTTPRequest) pluginapi.HTTPResponse {
		if out.URL != gatewayBase+"/organizations/team/chat/completions" || out.Headers.Get(organizationHeader) != "team" || out.Headers.Get("Authorization") != "Bearer fixture-token" {
			t.Fatal("wrong execution account")
		}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(out.Body, &payload); err != nil || string(payload["model"]) != `"selected/model"` || len(payload["tools"]) == 0 || len(payload["messages"]) == 0 {
			t.Fatalf("model or tools changed during execution: %v", err)
		}
		return pluginapi.HTTPResponse{StatusCode: status, Body: []byte(`{"choices":[{"message":{"tool_calls":[{"id":"fixture"}]}}]}`)}
	})
	response, err := Execute(t.Context(), request)
	if err != nil || !json.Valid(response.Payload) {
		t.Fatalf("chat did not return the gateway response: %v", err)
	}
	status = http.StatusTooManyRequests
	var upstream UpstreamError
	if _, err := Execute(t.Context(), request); !errors.As(err, &upstream) || upstream.StatusCode() != status {
		t.Fatalf("upstream 429 was lost: %v", err)
	}
	request.Stream = true
	stream, err := ChatRequest(request)
	if err != nil || stream.Headers.Get("Accept") != "text/event-stream" {
		t.Fatalf("stream did not request SSE: %v", err)
	}
}
