package kilo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

const gatewayBase = "https://ai-gateway.kilo.ai/api/v1"
const organizationHeader = "X-KiloCode-OrganizationId"

type gatewayAccount struct {
	base    string
	headers http.Header
}

func accountGateway(metadata map[string]any) (gatewayAccount, error) {
	token, _ := metadata["access_token"].(string)
	if token == "" || strings.ContainsAny(token, "\r\n") {
		return gatewayAccount{}, errors.New("Kilo account authorization is missing or invalid")
	}
	organization, _ := metadata["organization_id"].(string)
	if organization == "." || organization == ".." || strings.ContainsAny(organization, "/\\\r\n") {
		return gatewayAccount{}, errors.New("invalid Kilo organization identity")
	}
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+token)
	base := gatewayBase
	if organization != "" {
		base += "/organizations/" + url.PathEscape(organization)
		headers.Set(organizationHeader, organization)
	}
	return gatewayAccount{base: base, headers: headers}, nil
}

type gatewayModel struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Summary   string   `json:"description"`
	Context   int64    `json:"context_length"`
	MaxOutput int64    `json:"max_completion_tokens"`
	Params    []string `json:"supported_parameters"`
	Top       struct {
		MaxOutput int64 `json:"max_completion_tokens"`
	} `json:"top_provider"`
	Architecture struct {
		Input  []string `json:"input_modalities"`
		Output []string `json:"output_modalities"`
	} `json:"architecture"`
}

// ModelsForAuth discovers the current catalog for exactly one authorized account.
func ModelsForAuth(ctx context.Context, req pluginapi.AuthModelRequest) (pluginapi.ModelResponse, error) {
	if req.HTTPClient == nil || req.AuthProvider != providerID || req.AuthID == "" {
		return pluginapi.ModelResponse{}, errors.New("Kilo catalog requires an enrolled account and host transport")
	}
	account, err := accountGateway(req.Metadata)
	if err != nil {
		return pluginapi.ModelResponse{}, err
	}
	response, err := req.HTTPClient.Do(ctx, pluginapi.HTTPRequest{
		Method: http.MethodGet, URL: account.base + "/models", Headers: account.headers,
	})
	if err != nil {
		return pluginapi.ModelResponse{}, err
	}
	if response.StatusCode != http.StatusOK {
		return pluginapi.ModelResponse{}, fmt.Errorf("Kilo catalog returned HTTP %d", response.StatusCode)
	}
	var catalog struct {
		Data []gatewayModel `json:"data"`
	}
	if err := json.Unmarshal(response.Body, &catalog); err != nil || catalog.Data == nil {
		return pluginapi.ModelResponse{}, errors.New("invalid Kilo model catalog")
	}
	models := make([]pluginapi.ModelInfo, 0, len(catalog.Data))
	seen := make(map[string]bool, len(catalog.Data))
	for _, model := range catalog.Data {
		if model.ID == "" || model.ID != strings.TrimSpace(model.ID) || seen[model.ID] || model.Context < 0 {
			return pluginapi.ModelResponse{}, errors.New("Kilo returned an invalid or duplicate model identity")
		}
		seen[model.ID] = true
		limit := model.MaxOutput
		if limit == 0 {
			limit = model.Top.MaxOutput
		}
		models = append(models, pluginapi.ModelInfo{
			ID: model.ID, Name: model.ID, Object: "model", OwnedBy: providerID,
			DisplayName: model.Name, Description: model.Summary,
			ContextLength: model.Context, MaxCompletionTokens: limit,
			SupportedParameters: model.Params, SupportedInputModalities: model.Architecture.Input,
			SupportedOutputModalities: model.Architecture.Output,
		})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	return pluginapi.ModelResponse{Provider: providerID, Models: models}, nil
}
