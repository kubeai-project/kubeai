package openaiserver

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	k8sv1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/kubeai-project/kubeai/internal/apiutils"
	"github.com/kubeai-project/kubeai/internal/metrics/metricstest"
	"github.com/kubeai-project/kubeai/internal/modelproxy"
	"github.com/stretchr/testify/require"
)

type systemOneBackend struct {
	model                      *k8sv1.Model
	address                    string
	scaled, acquired, released atomic.Int32
}

func (b *systemOneBackend) LookupModel(_ context.Context, name, adapter string, _ []string) (*k8sv1.Model, error) {
	if name != "decision" {
		return nil, nil
	}
	return b.model, nil
}
func (b *systemOneBackend) ScaleAtLeastOneReplica(context.Context, string) error {
	b.scaled.Add(1)
	return nil
}
func (b *systemOneBackend) AwaitBestAddress(ctx context.Context, _ *apiutils.Request) (string, func(), error) {
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	b.acquired.Add(1)
	return b.address, func() { b.released.Add(1) }, nil
}

func TestSystemOneProxy(t *testing.T) {
	const payload = ` {"model":"decision","state":{"b":1,"a":2},"questions":{"urgent":{"type":"noul","instructions":"Urgent?"}},"extension":{"z":0,"a":1}} `
	var multipartBody bytes.Buffer
	w := multipart.NewWriter(&multipartBody)
	require.NoError(t, w.WriteField("request", payload))
	f, err := w.CreateFormFile("image", "example.png")
	require.NoError(t, err)
	_, err = f.Write([]byte{0, 1, 255, 13, 10})
	require.NoError(t, err)
	require.NoError(t, w.Close())
	type testCase struct {
		name, engine, body, contentType, method string
		backendStatus, wantStatus, attempts     int
		noFeature                               bool
	}
	cases := []testCase{
		{"infinity", k8sv1.InfinityEngine, payload, "application/json", "POST", 200, 400, 0, false},
		{"whisper", k8sv1.FasterWhisperEngine, payload, "application/json", "POST", 200, 400, 0, false},
		{"invalid JSON", k8sv1.VLLMEngine, "{", "application/json", "POST", 200, 400, 0, false},
		{"missing model", k8sv1.VLLMEngine, `{"model":"missing"}`, "application/json", "POST", 200, 404, 0, false},
		{"adapter", k8sv1.VLLMEngine, `{"model":"decision_adapter"}`, "application/json", "POST", 200, 400, 0, false},
		{"GET", k8sv1.VLLMEngine, payload, "application/json", "GET", 200, 405, 0, false},
		{"PUT", k8sv1.VLLMEngine, payload, "application/json", "PUT", 200, 405, 0, false},
		{"DELETE", k8sv1.VLLMEngine, payload, "application/json", "DELETE", 200, 405, 0, false},
	}
	for _, engine := range []string{k8sv1.OLlamaEngine, k8sv1.VLLMEngine, k8sv1.LlamaCppEngine, k8sv1.SGLangEngine} {
		for _, format := range []struct{ name, body, contentType string }{
			{"JSON", payload, "application/json"},
			{"multipart", multipartBody.String(), w.FormDataContentType()},
		} {
			for _, scenario := range []struct {
				name                                string
				backendStatus, wantStatus, attempts int
				noFeature                           bool
			}{
				{"success", 200, 200, 1, false},
				{"chunked", 200, 200, 1, false},
				{"chunked retry", 503, 200, 2, false},
				{"retry exhausted", 503, 503, 2, false},
				{"backend validation", 422, 422, 1, false},
				{"backend missing route", 404, 404, 1, false},
				{"missing feature", 200, 400, 0, true},
			} {
				cases = append(cases, testCase{scenario.name + "/" + engine + "/" + format.name, engine, format.body, format.contentType, "POST", scenario.backendStatus, scenario.wantStatus, scenario.attempts, scenario.noFeature})
			}
		}
	}
	for _, path := range []string{"/v1/systemone", "/openai/v1/systemone"} {
		for _, tc := range cases {
			t.Run(path+"/"+tc.name, func(t *testing.T) {
				metricstest.Init(t)
				var requests atomic.Int32
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					attempt := requests.Add(1)
					body, err := io.ReadAll(r.Body)
					if err != nil || string(body) != tc.body || r.URL.Path != apiutils.SystemOnePath || r.Header.Get("Content-Type") != tc.contentType || r.Header.Get("Authorization") != "Bearer test" || r.ContentLength != int64(len(tc.body)) || len(r.TransferEncoding) != 0 {
						t.Error("proxy changed the backend request", err)
					}
					w.Header().Set("Content-Type", "application/json")
					w.Header().Set("X-Backend", tc.engine)
					status := tc.backendStatus
					if status == 503 && attempt > 1 && !strings.Contains(tc.name, "exhausted") {
						status = 200
					}
					w.WriteHeader(status)
					fmt.Fprint(w, `{"answers":{"urgent":{"noul":0.8}},"diagnostics":{"extra":true}}`)
				}))
				defer upstream.Close()
				backend := &systemOneBackend{model: &k8sv1.Model{Spec: k8sv1.ModelSpec{Engine: tc.engine, Features: []k8sv1.ModelFeature{k8sv1.ModelFeatureSystemOne}}}, address: strings.TrimPrefix(upstream.URL, "http://")}
				if tc.noFeature {
					backend.model.Spec.Features = nil
				}
				h := NewHandler(nil, modelproxy.NewHandler(backend, backend, 1, nil))
				req := httptest.NewRequest(tc.method, path, strings.NewReader(tc.body))
				req.Header.Set("Content-Type", tc.contentType)
				req.Header.Set("Authorization", "Bearer test")
				if strings.HasPrefix(tc.name, "chunked") {
					// A real HTTP/1.1 request without a known body length arrives this way.
					req.ContentLength = -1
					req.TransferEncoding = []string{"chunked"}
				}
				response := httptest.NewRecorder()
				h.ServeHTTP(response, req)
				require.Equal(t, tc.wantStatus, response.Code, response.Body.String())
				require.EqualValues(t, tc.attempts, requests.Load())
				require.Equal(t, backend.acquired.Load(), backend.released.Load())
				if tc.attempts == 0 {
					require.Zero(t, backend.scaled.Load(), "invalid requests must not scale models")
				} else {
					require.EqualValues(t, 1, backend.scaled.Load())
					require.Equal(t, tc.engine, response.Header().Get("X-Backend"))
					require.Equal(t, `{"answers":{"urgent":{"noul":0.8}},"diagnostics":{"extra":true}}`, response.Body.String())
					metricstest.RequireActiveRequestsMetric(t, metricstest.Collect(t), "decision", 0)
				}
				if tc.wantStatus == 405 {
					require.Equal(t, "POST", response.Header().Get("Allow"))
				}
			})
		}
	}
}

func TestSystemOneDoesNotExposeOtherBackendRoutes(t *testing.T) {
	h := NewHandler(nil, nil)
	for _, path := range []string{"/openai/v1/systemone/admin", "/openai/v1/load_lora_adapter", "/openai/v1/decisions"} {
		response := httptest.NewRecorder()
		h.ServeHTTP(response, httptest.NewRequest("POST", path, nil))
		require.Equal(t, http.StatusNotFound, response.Code)
	}
}

func TestSystemOneCancellation(t *testing.T) {
	metricstest.Init(t)
	backend := &systemOneBackend{model: &k8sv1.Model{Spec: k8sv1.ModelSpec{
		Engine: k8sv1.SGLangEngine, Features: []k8sv1.ModelFeature{k8sv1.ModelFeatureSystemOne},
	}}}
	h := NewHandler(nil, modelproxy.NewHandler(backend, backend, 1, nil))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("POST", "/openai/v1/systemone", strings.NewReader(`{"model":"decision"}`)).WithContext(ctx)
	response := httptest.NewRecorder()
	h.ServeHTTP(response, req)
	require.Equal(t, http.StatusInternalServerError, response.Code)
	require.Zero(t, backend.acquired.Load())
	metricstest.RequireActiveRequestsMetric(t, metricstest.Collect(t), "decision", 0)
}

func TestSystemOneOversizedRequest(t *testing.T) {
	backend := &systemOneBackend{}
	h := NewHandler(nil, modelproxy.NewHandler(backend, backend, 1, nil))
	req := httptest.NewRequest("POST", "/openai/v1/systemone", strings.NewReader(strings.Repeat(" ", int(apiutils.MaxSystemOneBodyBytes)+1)))
	response := httptest.NewRecorder()
	h.ServeHTTP(response, req)
	require.Equal(t, http.StatusRequestEntityTooLarge, response.Code)
	require.Zero(t, backend.scaled.Load())
}
