package openaiserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	k8sv1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/kubeai-project/kubeai/internal/apiutils"
	"github.com/kubeai-project/kubeai/internal/metrics/metricstest"
	"github.com/kubeai-project/kubeai/internal/modelproxy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type messagesBackend struct {
	systemOneBackend
	name, adapter string
	selectors     []string
}

func (b *messagesBackend) LookupModel(_ context.Context, name, adapter string, selectors []string) (*k8sv1.Model, error) {
	b.name, b.adapter, b.selectors = name, adapter, selectors
	if name != "decision" {
		return nil, nil
	}
	return b.model, nil
}

func TestInferenceRoutes(t *testing.T) {
	for _, path := range []string{
		"/v1/rerank", "/openai/v1/rerank", "/v1/systemone", "/openai/v1/systemone",
		"/v1/messages", "/openai/v1/chat/completions", "/openai/v1/completions",
		"/openai/v1/embeddings", "/openai/v1/responses",
	} {
		t.Run(path, func(t *testing.T) {
			metricstest.Init(t)
			const body = ` {"model":"decision","extension":{"z":1,"a":2}} `
			var attempts atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				data, err := io.ReadAll(r.Body)
				if err != nil || string(data) != body || r.URL.Path != strings.TrimPrefix(path, "/openai") || r.URL.RawQuery != "trace=1" {
					t.Error("request changed", err)
				}
				fmt.Fprint(w, `{"ok":true}`)
			}))
			defer upstream.Close()
			backend := &systemOneBackend{model: &k8sv1.Model{Spec: k8sv1.ModelSpec{
				Engine: k8sv1.VLLMEngine, Features: []k8sv1.ModelFeature{k8sv1.ModelFeatureSystemOne, k8sv1.ModelFeatureTextGeneration},
			}}, address: strings.TrimPrefix(upstream.URL, "http://")}
			h := NewHandler(nil, modelproxy.NewHandler(backend, backend, 0, nil))
			response := httptest.NewRecorder()
			h.ServeHTTP(response, httptest.NewRequest("POST", path+"?trace=1", strings.NewReader(body)))
			require.Equal(t, 200, response.Code, response.Body.String())
			require.EqualValues(t, 1, attempts.Load())
			if path == "/openai/v1/rerank" || path == "/openai/v1/systemone" {
				require.Equal(t, "@1791417600", response.Header().Get("Deprecation"))
				require.Contains(t, response.Header().Get("Link"), "<"+strings.TrimPrefix(path, "/openai")+">")
			} else {
				require.Empty(t, response.Header().Get("Deprecation"))
			}
		})
	}
}

func TestMessagesProxy(t *testing.T) {
	const body = ` {"model":"decision","max_tokens":128,"system":[{"type":"text","text":"help","cache_control":{"type":"ephemeral"}}],"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","data":"abc"}},{"type":"text","text":"hi"}]}],"tools":[{"name":"test","input_schema":{"z":1,"a":2}}],"stream":true,"extension":true} `
	for _, tc := range []struct {
		name                   string
		status, want, attempts int
		adapter                bool
	}{
		{"success", 200, 200, 1, false}, {"retry", 503, 200, 2, false},
		{"exhausted", 503, 503, 2, false}, {"backend validation", 422, 422, 1, false},
		{"backend missing route", 404, 404, 1, false}, {"adapter", 200, 200, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			metricstest.Init(t)
			payload := body
			if tc.adapter {
				payload = strings.Replace(body, `"decision"`, `"decision_adapter"`, 1)
			}
			var attempts atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempt := attempts.Add(1)
				data, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				if tc.adapter {
					assert.JSONEq(t, strings.Replace(body, `"decision"`, `"adapter"`, 1), string(data))
				} else {
					assert.Equal(t, body, string(data))
				}
				for key, value := range map[string]string{"Anthropic-Version": "2023-06-01", "Anthropic-Beta": "test-beta", "X-Api-Key": "test", "Content-Type": "application/json"} {
					if r.Header.Get(key) != value {
						t.Errorf("lost header %s", key)
					}
				}
				if r.URL.Path != "/v1/messages" || r.ContentLength != int64(len(data)) || len(r.TransferEncoding) != 0 {
					t.Error("incorrect upstream request")
				}
				status := tc.status
				if tc.name == "retry" && attempt > 1 {
					status = 200
				}
				w.Header().Set("Content-Type", "text/event-stream")
				w.Header().Set("Request-Id", "backend-id")
				w.WriteHeader(status)
				fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\"}\n\n")
			}))
			defer upstream.Close()
			backend := &messagesBackend{systemOneBackend: systemOneBackend{model: &k8sv1.Model{Spec: k8sv1.ModelSpec{Features: []k8sv1.ModelFeature{k8sv1.ModelFeatureTextGeneration}}}, address: strings.TrimPrefix(upstream.URL, "http://")}}
			h := NewHandler(nil, modelproxy.NewHandler(backend, backend, 1, nil))
			r := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(payload))
			r.ContentLength, r.TransferEncoding = -1, []string{"chunked"}
			for key, value := range map[string]string{"Anthropic-Version": "2023-06-01", "Anthropic-Beta": "test-beta", "X-Api-Key": "test", "Content-Type": "application/json", "X-Label-Selector": "team=one"} {
				r.Header.Set(key, value)
			}
			response := httptest.NewRecorder()
			h.ServeHTTP(response, r)
			require.Equal(t, tc.want, response.Code)
			require.EqualValues(t, tc.attempts, attempts.Load())
			require.Equal(t, "backend-id", response.Header().Get("Request-Id"))
			require.Equal(t, "text/event-stream", response.Header().Get("Content-Type"))
			require.Equal(t, "event: message_start\ndata: {\"type\":\"message_start\"}\n\n", response.Body.String())
			require.Equal(t, []string{"team=one"}, backend.selectors)
			require.Equal(t, "decision", backend.name)
			if tc.adapter {
				require.Equal(t, "adapter", backend.adapter)
			}
			require.EqualValues(t, 1, backend.scaled.Load())
			require.Equal(t, backend.acquired.Load(), backend.released.Load())
			requestedModel := "decision"
			if tc.adapter {
				requestedModel += "_adapter"
			}
			metricstest.RequireActiveRequestsMetric(t, metricstest.Collect(t), requestedModel, 0)
		})
	}
}

func TestMessagesErrors(t *testing.T) {
	for _, tc := range []struct {
		name, method, body, contentType, errorType string
		status                                     int
		noFeature                                  bool
	}{
		{"method", "GET", `{}`, "application/json", "invalid_request_error", 405, false},
		{"invalid JSON", "POST", `{`, "application/json", "invalid_request_error", 400, false},
		{"missing model", "POST", `{}`, "application/json", "invalid_request_error", 400, false},
		{"blank model", "POST", `{"model":" "}`, "application/json", "invalid_request_error", 400, false},
		{"non-string model", "POST", `{"model":42}`, "application/json", "invalid_request_error", 400, false},
		{"duplicate model", "POST", `{"model":"decision","model":"other"}`, "application/json", "invalid_request_error", 400, false},
		{"media type", "POST", `{"model":"decision"}`, "multipart/form-data; boundary=x", "invalid_request_error", 400, false},
		{"invalid media type", "POST", `{}`, "application/json;broken", "invalid_request_error", 400, false},
		{"unknown model", "POST", `{"model":"other"}`, "application/json", "not_found_error", 404, false},
		{"feature", "POST", `{"model":"decision"}`, "application/json", "invalid_request_error", 400, true},
		{"oversize", "POST", strings.Repeat(" ", (32<<20)+1), "application/json", "request_too_large", 413, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &messagesBackend{systemOneBackend: systemOneBackend{model: &k8sv1.Model{Spec: k8sv1.ModelSpec{Features: []k8sv1.ModelFeature{k8sv1.ModelFeatureTextGeneration}}}}}
			if tc.noFeature {
				backend.model.Spec.Features = nil
			}
			h := NewHandler(nil, modelproxy.NewHandler(backend, backend, 0, nil))
			r := httptest.NewRequest(tc.method, "/v1/messages", strings.NewReader(tc.body))
			r.Header.Set("Content-Type", tc.contentType)
			response := httptest.NewRecorder()
			h.ServeHTTP(response, r)
			require.Equal(t, tc.status, response.Code)
			require.Equal(t, "application/json", response.Header().Get("Content-Type"))
			var result struct {
				Type  string
				Error struct{ Type, Message string }
			}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
			require.Equal(t, "error", result.Type)
			require.Equal(t, tc.errorType, result.Error.Type)
			require.NotEmpty(t, result.Error.Message)
			require.Zero(t, backend.scaled.Load())
			require.Zero(t, backend.acquired.Load())
			if tc.status == 405 {
				require.Equal(t, "POST", response.Header().Get("Allow"))
			}
		})
	}
}

type messagesFailureBackend struct {
	messagesBackend
	scaleError, addressError error
}

func (b *messagesFailureBackend) ScaleAtLeastOneReplica(context.Context, string) error {
	return b.scaleError
}

func (b *messagesFailureBackend) AwaitBestAddress(context.Context, *apiutils.Request) (string, func(), error) {
	return "", nil, b.addressError
}

func TestMessagesLifecycleErrors(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		scaleError, addressError error
		status                   int
	}{
		{"scaling", fmt.Errorf("private scaling details"), nil, 500},
		{"deadline", nil, context.DeadlineExceeded, 504},
		{"cancelled", nil, context.Canceled, 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			metricstest.Init(t)
			backend := &messagesFailureBackend{
				messagesBackend: messagesBackend{systemOneBackend: systemOneBackend{model: &k8sv1.Model{Spec: k8sv1.ModelSpec{Features: []k8sv1.ModelFeature{k8sv1.ModelFeatureTextGeneration}}}}},
				scaleError:      tc.scaleError, addressError: tc.addressError,
			}
			h := NewHandler(nil, modelproxy.NewHandler(backend, backend, 0, nil))
			response := httptest.NewRecorder()
			h.ServeHTTP(response, httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"model":"decision"}`)))
			require.Equal(t, tc.status, response.Code)
			require.JSONEq(t, fmt.Sprintf(`{"type":"error","error":{"type":"api_error","message":%q}}`, http.StatusText(tc.status)), response.Body.String())
			metricstest.RequireActiveRequestsMetric(t, metricstest.Collect(t), "decision", 0)
		})
	}
}

func TestDeprecatedRouteErrors(t *testing.T) {
	h := NewHandler(nil, modelproxy.NewHandler(nil, nil, 0, nil))
	for _, path := range []string{"/openai/v1/rerank", "/openai/v1/systemone"} {
		response := httptest.NewRecorder()
		h.ServeHTTP(response, httptest.NewRequest("POST", path, strings.NewReader(`{}`)))
		require.Equal(t, 400, response.Code)
		require.Equal(t, "@1791417600", response.Header().Get("Deprecation"))
		require.Contains(t, response.Header().Get("Link"), strings.TrimPrefix(path, "/openai"))
	}
}

func TestMessagesStreamingFlush(t *testing.T) {
	metricstest.Init(t)
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: message_start\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, "event: message_stop\n\n")
	}))
	defer upstream.Close()
	backend := &messagesBackend{systemOneBackend: systemOneBackend{model: &k8sv1.Model{Spec: k8sv1.ModelSpec{Features: []k8sv1.ModelFeature{k8sv1.ModelFeatureTextGeneration}}}, address: strings.TrimPrefix(upstream.URL, "http://")}}
	proxy := httptest.NewServer(NewHandler(nil, modelproxy.NewHandler(backend, backend, 0, nil)))
	defer proxy.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r, err := http.NewRequestWithContext(ctx, "POST", proxy.URL+"/v1/messages", strings.NewReader(`{"model":"decision","stream":true}`))
	require.NoError(t, err)
	response, err := proxy.Client().Do(r)
	require.NoError(t, err)
	defer response.Body.Close()
	reader := bufio.NewReader(response.Body)
	line, err := reader.ReadString('\n')
	require.NoError(t, err, "first event must arrive before the backend finishes")
	require.Equal(t, "event: message_start\n", line)
	close(release)
	rest, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Equal(t, "\nevent: message_stop\n\n", string(rest))
}

func TestAPIRouteAllowlist(t *testing.T) {
	h := NewHandler(nil, nil)
	for _, path := range []string{"/v1/load_lora_adapter", "/v1/messages/count_tokens", "/v1/messages/admin", "/v1/systemone/admin", "/v1/rerank/admin", "/v1/chat/completions", "/openai/v1/messages"} {
		response := httptest.NewRecorder()
		h.ServeHTTP(response, httptest.NewRequest("POST", path, nil))
		require.Equal(t, 404, response.Code, path)
	}
}
