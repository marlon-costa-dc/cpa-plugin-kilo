package kilo

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

type fixtureTransport func(pluginapi.HTTPRequest) pluginapi.HTTPResponse

func (f fixtureTransport) Do(_ context.Context, req pluginapi.HTTPRequest) (pluginapi.HTTPResponse, error) {
	return f(req), nil
}

func (fixtureTransport) DoStream(context.Context, pluginapi.HTTPRequest) (pluginapi.HTTPStreamResponse, error) {
	panic("device authorization must not stream")
}

func TestDeviceAuthorizationBindsSelectedOrganization(t *testing.T) {
	ctx := t.Context()
	pending := true
	transport := fixtureTransport(func(req pluginapi.HTTPRequest) pluginapi.HTTPResponse {
		switch req.URL {
		case apiOrigin + devicePath:
			if req.Method != http.MethodPost {
				t.Fatal("device login did not start with POST")
			}
			return pluginapi.HTTPResponse{StatusCode: 201, Body: []byte(`{"code":"ABCD","verificationUrl":"https://kilo.ai/auth/device","expiresIn":300}`)}
		case apiOrigin + devicePath + "/ABCD":
			if pending {
				return pluginapi.HTTPResponse{StatusCode: 202}
			}
			return pluginapi.HTTPResponse{StatusCode: 200, Body: []byte(`{"status":"approved","token":"fixture-token"}`)}
		case apiOrigin + "/api/profile":
			if req.Headers.Get("Authorization") != "Bearer fixture-token" {
				t.Fatal("profile did not use account authorization")
			}
			return pluginapi.HTTPResponse{StatusCode: 200, Body: []byte(`{"email":"fixture@example.invalid","organizations":[{"id":"org-one"},{"id":"org-two"}]}`)}
		default:
			t.Fatal("unexpected device API request")
			return pluginapi.HTTPResponse{}
		}
	})
	start, err := StartLogin(ctx, pluginapi.AuthLoginStartRequest{Provider: providerID, HTTPClient: transport, Metadata: map[string]any{"organization_id": "org-two"}})
	if err != nil || start.UserCode != "ABCD" || start.State == start.UserCode || start.ExpiresAt.IsZero() {
		t.Fatalf("invalid public authorization: %v", err)
	}
	request := pluginapi.AuthLoginPollRequest{Provider: providerID, State: start.State, HTTPClient: transport, Metadata: start.Metadata}
	result, err := PollLogin(ctx, request)
	if err != nil || result.Status != pluginapi.AuthLoginStatusPending {
		t.Fatalf("poll did not return pending: %v", err)
	}
	pending = false
	result, err = PollLogin(ctx, request)
	if err != nil || result.Status != pluginapi.AuthLoginStatusSuccess {
		t.Fatalf("approved account was not accepted: %v", err)
	}
	if result.Auth.Metadata["organization_id"] != "org-two" || strings.Contains(result.Auth.FileName, "@") {
		t.Fatal("authorization did not bind selected organization and private storage identity")
	}
	request.Metadata["organization_id"] = ""
	result, err = PollLogin(ctx, request)
	if err != nil || result.Status != pluginapi.AuthLoginStatusError || result.Auth.ID != "" {
		t.Fatal("ambiguous organization did not fail closed")
	}
}
