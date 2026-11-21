package kilo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

// ChatRequest carries the selected model and the account's authorization to Kilo.
func ChatRequest(req pluginapi.ExecutorRequest) (pluginapi.HTTPRequest, error) {
	if req.AuthProvider != providerID || req.AuthID == "" || req.Model == "" {
		return pluginapi.HTTPRequest{}, errors.New("Kilo execution requires an enrolled account and selected model")
	}
	account, err := accountGateway(req.AuthMetadata)
	if err != nil {
		return pluginapi.HTTPRequest{}, err
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(req.Payload, &payload); err != nil || payload == nil {
		return pluginapi.HTTPRequest{}, errors.New("invalid Kilo chat payload")
	}
	model, err := json.Marshal(req.Model)
	if err != nil {
		return pluginapi.HTTPRequest{}, err
	}
	payload["model"] = model
	if req.Stream {
		payload["stream"] = json.RawMessage(`true`)
		account.headers.Set("Accept", "text/event-stream")
	} else {
		payload["stream"] = json.RawMessage(`false`)
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return pluginapi.HTTPRequest{}, err
	}
	account.headers.Set("Content-Type", "application/json")
	return pluginapi.HTTPRequest{Method: http.MethodPost, URL: account.base + "/chat/completions", Headers: account.headers, Body: body}, nil
}

// UpstreamError retains the status without exposing credential-bearing bodies.
type UpstreamError int

func (e UpstreamError) Error() string   { return fmt.Sprintf("Kilo chat returned HTTP %d", e) }
func (e UpstreamError) StatusCode() int { return int(e) }

// Execute sends a non-streaming request through the host's credential scope.
func Execute(ctx context.Context, req pluginapi.ExecutorRequest) (pluginapi.ExecutorResponse, error) {
	if req.HTTPClient == nil || req.Stream {
		return pluginapi.ExecutorResponse{}, errors.New("Kilo execution requires non-streaming host transport")
	}
	request, err := ChatRequest(req)
	if err != nil {
		return pluginapi.ExecutorResponse{}, err
	}
	response, err := req.HTTPClient.Do(ctx, request)
	if err != nil {
		return pluginapi.ExecutorResponse{}, err
	}
	if response.StatusCode != http.StatusOK {
		return pluginapi.ExecutorResponse{}, UpstreamError(response.StatusCode)
	}
	if !json.Valid(response.Body) {
		return pluginapi.ExecutorResponse{}, errors.New("invalid Kilo chat response")
	}
	return pluginapi.ExecutorResponse{Payload: response.Body, Headers: response.Headers}, nil
}
