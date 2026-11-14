// Package kilo implements the Kilo account device authorization for the native plugin ABI.
package kilo

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

const providerID = "kilo"
const apiOrigin = "https://api.kilo.ai"
const devicePath = "/api/device-auth/codes"

// LoginStart is the additive native wire contract for public device codes.
// Hosts before the device-code change can load the plugin but cannot display it.
type LoginStart struct {
	pluginapi.AuthLoginStartResponse
	UserCode string
}

// StartLogin returns a public device code while keeping the polling state on the host.
func StartLogin(ctx context.Context, req pluginapi.AuthLoginStartRequest) (LoginStart, error) {
	if req.HTTPClient == nil || req.Provider != providerID {
		return LoginStart{}, errors.New("Kilo login requires its host HTTP transport")
	}
	var result struct {
		Code    string `json:"code"`
		URL     string `json:"verificationUrl"`
		Expires int    `json:"expiresIn"`
	}
	if err := call(ctx, req.HTTPClient, http.MethodPost, devicePath, "", &result); err != nil {
		return LoginStart{}, err
	}
	verify, err := url.Parse(result.URL)
	if err != nil || verify.Scheme != "https" || verify.User != nil || !allowedVerificationHost(verify.Hostname()) || result.Code == "" || result.Expires < 1 || result.Expires > 3600 {
		return LoginStart{}, errors.New("invalid Kilo device authorization")
	}
	organization, _ := req.Metadata["organization_id"].(string)
	return LoginStart{UserCode: result.Code, AuthLoginStartResponse: pluginapi.AuthLoginStartResponse{
		Provider: providerID, URL: result.URL, State: rand.Text(),
		ExpiresAt: time.Now().UTC().Add(time.Duration(result.Expires) * time.Second),
		Metadata:  map[string]any{"device_code": result.Code, "organization_id": organization},
	}}, nil
}

func allowedVerificationHost(host string) bool {
	return host == "kilo.ai" || strings.HasSuffix(host, ".kilo.ai")
}

// PollLogin authorizes exactly one account and explicitly selected organization.
func PollLogin(ctx context.Context, req pluginapi.AuthLoginPollRequest) (pluginapi.AuthLoginPollResponse, error) {
	code, _ := req.Metadata["device_code"].(string)
	if req.HTTPClient == nil || req.Provider != providerID || req.State == "" || code == "" {
		return pluginapi.AuthLoginPollResponse{}, errors.New("invalid Kilo polling context")
	}
	var result struct {
		Status string `json:"status"`
		Token  string `json:"token"`
	}
	if err := call(ctx, req.HTTPClient, http.MethodGet, devicePath+"/"+url.PathEscape(code), "", &result); err != nil {
		var status httpFailure
		if !errors.As(err, &status) {
			return pluginapi.AuthLoginPollResponse{}, err
		}
		switch status {
		case http.StatusAccepted:
			return pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusPending}, nil
		case http.StatusForbidden, http.StatusGone:
			return pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusError, Message: "Kilo authorization was denied or expired"}, nil
		default:
			return pluginapi.AuthLoginPollResponse{}, err
		}
	}
	if result.Status == "pending" {
		return pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusPending}, nil
	}
	if result.Status == "denied" || result.Status == "expired" {
		return pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusError, Message: "Kilo authorization was denied or expired"}, nil
	}
	if result.Status != "approved" || result.Token == "" {
		return pluginapi.AuthLoginPollResponse{}, errors.New("Kilo authorization did not return an account token")
	}
	return approved(ctx, req, result.Token)
}

func approved(ctx context.Context, req pluginapi.AuthLoginPollRequest, token string) (pluginapi.AuthLoginPollResponse, error) {
	var profile struct {
		Email         string `json:"email"`
		Organizations []struct {
			ID string `json:"id"`
		} `json:"organizations"`
	}
	if err := call(ctx, req.HTTPClient, http.MethodGet, "/api/profile", token, &profile); err != nil {
		return pluginapi.AuthLoginPollResponse{}, err
	}
	if strings.TrimSpace(profile.Email) == "" {
		return pluginapi.AuthLoginPollResponse{}, errors.New("Kilo profile did not identify an account")
	}
	organization, _ := req.Metadata["organization_id"].(string)
	if organization == "" && len(profile.Organizations) > 1 {
		return pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusError, Message: "Select a Kilo organization before signing in"}, nil
	}
	if organization == "" && len(profile.Organizations) == 1 {
		organization = profile.Organizations[0].ID
	}
	if organization != "" {
		matched := false
		for _, org := range profile.Organizations {
			if org.ID == organization {
				matched = true
				break
			}
		}
		if !matched {
			return pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusError, Message: "Kilo account does not belong to the selected organization"}, nil
		}
	}
	identity := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(profile.Email)) + "\x00" + organization))
	name := providerID + "-" + hex.EncodeToString(identity[:16]) + ".json"
	return pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusSuccess, Auth: pluginapi.AuthData{
		Provider: providerID, ID: name, FileName: name,
		Metadata: map[string]any{"type": providerID, "access_token": token, "email": profile.Email, "organization_id": organization},
	}}, nil
}

type httpFailure int

func (e httpFailure) Error() string { return fmt.Sprintf("Kilo device API returned HTTP %d", e) }

func call(ctx context.Context, client pluginapi.HostHTTPClient, method, path, token string, target any) error {
	headers := http.Header{"Content-Type": []string{"application/json"}}
	if token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(ctx, pluginapi.HTTPRequest{Method: method, URL: apiOrigin + path, Headers: headers})
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		return httpFailure(response.StatusCode)
	}
	if err := json.Unmarshal(response.Body, target); err != nil {
		return errors.New("invalid Kilo device API response")
	}
	return nil
}
