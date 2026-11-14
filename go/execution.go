package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/marlon-costa-dc/cpa-plugin-kilo"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

func executeChat(method string, raw []byte) ([]byte, error) {
	var input struct {
		pluginapi.ExecutorRequest
		Callback string `json:"host_callback_id"`
		StreamID string `json:"stream_id"`
	}
	if err := json.Unmarshal(raw, &input); err != nil || input.Callback == "" {
		return nil, errors.New("invalid Kilo executor input")
	}
	input.HTTPClient = hostHTTP(input.Callback)
	if method == pluginabi.MethodExecutorExecute {
		response, err := kilo.Execute(context.Background(), input.ExecutorRequest)
		if err != nil {
			return nil, err
		}
		return okEnvelope(response)
	}
	if input.StreamID == "" {
		return nil, errors.New("missing Kilo stream owner")
	}
	input.Stream = true
	request, err := kilo.ChatRequest(input.ExecutorRequest)
	if err != nil {
		return nil, err
	}
	response, err := callHost(pluginabi.MethodHostHTTPDoStream, struct {
		Callback string      `json:"host_callback_id"`
		Method   string      `json:"method"`
		URL      string      `json:"url"`
		Headers  http.Header `json:"headers"`
		Body     []byte      `json:"body"`
	}{input.Callback, request.Method, request.URL, request.Headers, request.Body})
	if err != nil {
		return nil, errors.New("Kilo host stream open failed")
	}
	var upstream struct {
		ID      string      `json:"stream_id"`
		Status  int         `json:"status_code"`
		Headers http.Header `json:"headers"`
	}
	if json.Unmarshal(response, &upstream) != nil || upstream.ID == "" {
		return nil, errors.New("invalid CLIProxy HTTP stream response")
	}
	if upstream.Status != http.StatusOK {
		_, _ = callHost(pluginabi.MethodHostHTTPStreamClose, streamRef{upstream.ID})
		return nil, kilo.UpstreamError(upstream.Status)
	}
	ack, err := okEnvelope(map[string]any{"headers": upstream.Headers})
	if err != nil {
		_, _ = callHost(pluginabi.MethodHostHTTPStreamClose, streamRef{upstream.ID})
		return nil, err
	}
	go forwardStream(upstream.ID, input.StreamID)
	return ack, nil
}

type streamRef struct {
	ID string `json:"stream_id"`
}

func forwardStream(upstream, downstream string) {
	defer func() { _, _ = callHost(pluginabi.MethodHostHTTPStreamClose, streamRef{upstream}) }()
	finished := false
	defer func() {
		result := map[string]any{"stream_id": downstream}
		if !finished {
			result["error_detail"] = pluginabi.Error{Code: pluginabi.ErrorCodeStreamTruncated, Message: "Kilo upstream stream ended with an error"}
		}
		_, _ = callHost(pluginabi.MethodHostStreamClose, result)
	}()
	for {
		raw, err := callHost(pluginabi.MethodHostHTTPStreamRead, streamRef{upstream})
		if err != nil {
			return
		}
		var chunk struct {
			Payload []byte `json:"payload"`
			Error   string `json:"error"`
			Done    bool   `json:"done"`
		}
		if json.Unmarshal(raw, &chunk) != nil || chunk.Error != "" {
			return
		}
		if len(chunk.Payload) != 0 {
			if _, err := callHost(pluginabi.MethodHostStreamEmit, map[string]any{"stream_id": downstream, "payload": chunk.Payload}); err != nil {
				return
			}
		}
		if chunk.Done {
			finished = true
			return
		}
	}
}

func executionError(err error) []byte {
	var upstream kilo.UpstreamError
	status := 0
	if errors.As(err, &upstream) {
		status = upstream.StatusCode()
	}
	raw, _ := json.Marshal(envelope{OK: false, Error: &envelopeError{Code: "plugin_error", Message: err.Error(), Status: status}})
	return raw
}
