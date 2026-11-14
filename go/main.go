package main

/*
#include <stdint.h>
#include <stdlib.h>

typedef struct {
	void* ptr;
	size_t len;
} cliproxy_buffer;

typedef int (*cliproxy_host_call_fn)(void*, const char*, const uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_host_free_fn)(void*, size_t);

typedef struct {
	uint32_t abi_version;
	void* host_ctx;
	cliproxy_host_call_fn call;
	cliproxy_host_free_fn free_buffer;
} cliproxy_host_api;

typedef int (*cliproxy_plugin_call_fn)(char*, uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_plugin_free_fn)(void*, size_t);
typedef void (*cliproxy_plugin_shutdown_fn)(void);

typedef struct {
	uint32_t abi_version;
	cliproxy_plugin_call_fn call;
	cliproxy_plugin_free_fn free_buffer;
	cliproxy_plugin_shutdown_fn shutdown;
} cliproxy_plugin_api;

extern int cliproxyPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void cliproxyPluginFree(void*, size_t);
extern void cliproxyPluginShutdown(void);

static const cliproxy_host_api* stored_host;

static void store_host_api(const cliproxy_host_api* host) {
	stored_host = host;
}

static int call_host_api(const char* method, const uint8_t* request, size_t request_len, cliproxy_buffer* response) {
	if (stored_host == NULL || stored_host->call == NULL) {
		return 1;
	}
	return stored_host->call(stored_host->host_ctx, method, request, request_len, response);
}

static void free_host_buffer(void* ptr, size_t len) {
	if (stored_host != NULL && stored_host->free_buffer != NULL && ptr != NULL) {
		stored_host->free_buffer(ptr, len);
	}
}
*/
import "C"

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"unsafe"

	"github.com/marlon-costa-dc/cpa-plugin-kilo"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/pluginapi"
)

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}
type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"http_status,omitempty"`
}

func main() {}

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
	if plugin == nil {
		return 1
	}
	C.store_host_api(host)
	plugin.abi_version = C.uint32_t(pluginabi.ABIVersion)
	plugin.call = C.cliproxy_plugin_call_fn(C.cliproxyPluginCall)
	plugin.free_buffer = C.cliproxy_plugin_free_fn(C.cliproxyPluginFree)
	plugin.shutdown = C.cliproxy_plugin_shutdown_fn(C.cliproxyPluginShutdown)
	return 0
}

//export cliproxyPluginCall
func cliproxyPluginCall(method *C.char, request *C.uint8_t, requestLen C.size_t, response *C.cliproxy_buffer) C.int {
	if response != nil {
		response.ptr = nil
		response.len = 0
	}
	if method == nil {
		writeResponse(response, errorEnvelope("invalid_method", "method is required"))
		return 1
	}
	var requestBytes []byte
	if request != nil && requestLen > 0 {
		requestBytes = C.GoBytes(unsafe.Pointer(request), C.int(requestLen))
	}
	raw, errHandle := handleMethod(C.GoString(method), requestBytes)
	if errHandle != nil {
		writeResponse(response, executionError(errHandle))
		return 1
	}
	writeResponse(response, raw)
	return 0
}

//export cliproxyPluginFree
func cliproxyPluginFree(ptr unsafe.Pointer, len C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
	_ = len
}

//export cliproxyPluginShutdown
func cliproxyPluginShutdown() {}

const providerID = "kilo"

type hostHTTP string

func (h hostHTTP) Do(_ context.Context, request pluginapi.HTTPRequest) (pluginapi.HTTPResponse, error) {
	if h == "" {
		return pluginapi.HTTPResponse{}, errors.New("missing CLIProxy HTTP ownership context")
	}
	raw, err := callHost(pluginabi.MethodHostHTTPDo, struct {
		Callback string      `json:"host_callback_id"`
		Method   string      `json:"method"`
		URL      string      `json:"url"`
		Headers  http.Header `json:"headers"`
		Body     []byte      `json:"body"`
	}{string(h), request.Method, request.URL, request.Headers, request.Body})
	if err != nil {
		return pluginapi.HTTPResponse{}, errors.New("Kilo host HTTP request failed")
	}
	var result pluginapi.HTTPResponse
	if err = json.Unmarshal(raw, &result); err != nil {
		return pluginapi.HTTPResponse{}, errors.New("invalid CLIProxy HTTP response")
	}
	return result, nil
}

func (hostHTTP) DoStream(context.Context, pluginapi.HTTPRequest) (pluginapi.HTTPStreamResponse, error) {
	return pluginapi.HTTPStreamResponse{}, errors.New("Kilo device authorization does not stream")
}

func handleMethod(method string, request []byte) ([]byte, error) {
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		return okEnvelope(map[string]any{
			"schema_version": pluginabi.SchemaVersion,
			"metadata": pluginapi.Metadata{
				Name: "Kilo OAuth", Version: "0.1.0", Author: "DataCosmos",
				GitHubRepository: "https://github.com/marlon-costa-dc/cpa-plugin-kilo",
			},
			"capabilities": map[string]any{
				"auth_provider": true, "model_provider": true, "executor": true,
				"executor_model_scope":    pluginapi.ExecutorModelScopeOAuth,
				"executor_input_formats":  []string{"chat-completions"},
				"executor_output_formats": []string{"chat-completions"},
			},
		})
	case pluginabi.MethodAuthIdentifier:
		return okEnvelope(map[string]string{"identifier": providerID})
	case pluginabi.MethodExecutorIdentifier:
		return okEnvelope(map[string]string{"identifier": providerID})
	case pluginabi.MethodExecutorExecute, pluginabi.MethodExecutorExecuteStream:
		return executeChat(method, request)
	case pluginabi.MethodExecutorCountTokens:
		return errorEnvelope(pluginabi.ErrorCodeNotImplemented, "Kilo token counting is unavailable"), nil
	case pluginabi.MethodModelStatic:
		return okEnvelope(pluginapi.ModelResponse{Provider: providerID, Models: []pluginapi.ModelInfo{}})
	case pluginabi.MethodModelForAuth:
		var input struct {
			pluginapi.AuthModelRequest
			Callback string `json:"host_callback_id"`
		}
		if err := json.Unmarshal(request, &input); err != nil {
			return nil, errors.New("invalid Kilo catalog input")
		}
		input.HTTPClient = hostHTTP(input.Callback)
		result, err := kilo.ModelsForAuth(context.Background(), input.AuthModelRequest)
		if err != nil {
			return nil, err
		}
		return okEnvelope(result)
	case pluginabi.MethodAuthLoginStart:
		var input struct {
			pluginapi.AuthLoginStartRequest
			Callback string `json:"host_callback_id"`
		}
		if err := json.Unmarshal(request, &input); err != nil {
			return nil, errors.New("invalid Kilo start input")
		}
		input.HTTPClient = hostHTTP(input.Callback)
		result, err := kilo.StartLogin(context.Background(), input.AuthLoginStartRequest)
		if err != nil {
			return nil, err
		}
		return okEnvelope(result)
	case pluginabi.MethodAuthLoginPoll:
		var input struct {
			pluginapi.AuthLoginPollRequest
			Callback string `json:"host_callback_id"`
		}
		if err := json.Unmarshal(request, &input); err != nil {
			return nil, errors.New("invalid Kilo poll input")
		}
		input.HTTPClient = hostHTTP(input.Callback)
		result, err := kilo.PollLogin(context.Background(), input.AuthLoginPollRequest)
		if err != nil {
			return nil, err
		}
		return okEnvelope(result)
	case pluginabi.MethodAuthParse:
		return okEnvelope(pluginapi.AuthParseResponse{Handled: false})
	case pluginabi.MethodAuthRefresh:
		return nil, errors.New("Kilo account renewal requires authorization")
	default:
		return nil, errors.New("unsupported Kilo plugin operation")
	}
}

func callHost(method string, payload any) (json.RawMessage, error) {
	rawPayload, errMarshal := json.Marshal(payload)
	if errMarshal != nil {
		return nil, fmt.Errorf("marshal host callback payload %s: %w", method, errMarshal)
	}
	cMethod := C.CString(method)
	defer C.free(unsafe.Pointer(cMethod))

	var response C.cliproxy_buffer
	var requestPtr *C.uint8_t
	if len(rawPayload) > 0 {
		cPayload := C.CBytes(rawPayload)
		if cPayload == nil {
			return nil, fmt.Errorf("allocate host callback payload %s", method)
		}
		defer C.free(cPayload)
		requestPtr = (*C.uint8_t)(cPayload)
	}
	callCode := C.call_host_api(cMethod, requestPtr, C.size_t(len(rawPayload)), &response)
	var rawResponse []byte
	if response.ptr != nil && response.len > 0 {
		rawResponse = C.GoBytes(response.ptr, C.int(response.len))
	}
	if response.ptr != nil {
		C.free_host_buffer(response.ptr, response.len)
	}
	if len(rawResponse) == 0 {
		return nil, fmt.Errorf("host callback %s returned no response, code=%d", method, int(callCode))
	}

	var env envelope
	if errUnmarshal := json.Unmarshal(rawResponse, &env); errUnmarshal != nil {
		return nil, fmt.Errorf("decode host callback envelope %s: %w", method, errUnmarshal)
	}
	if !env.OK {
		if env.Error != nil {
			return nil, fmt.Errorf("%s: %s", env.Error.Code, env.Error.Message)
		}
		return nil, fmt.Errorf("host callback %s failed", method)
	}
	if callCode != 0 {
		return nil, fmt.Errorf("host callback %s returned code=%d", method, int(callCode))
	}
	return append(json.RawMessage(nil), env.Result...), nil
}

func okEnvelope(v any) ([]byte, error) {
	raw, errMarshal := json.Marshal(v)
	if errMarshal != nil {
		return nil, errMarshal
	}
	return json.Marshal(envelope{OK: true, Result: raw})
}

func errorEnvelope(code, message string) []byte {
	raw, _ := json.Marshal(envelope{OK: false, Error: &envelopeError{Code: code, Message: message}})
	return raw
}

func writeResponse(response *C.cliproxy_buffer, raw []byte) {
	if response == nil || len(raw) == 0 {
		return
	}
	ptr := C.CBytes(raw)
	if ptr == nil {
		return
	}
	response.ptr = ptr
	response.len = C.size_t(len(raw))
}
