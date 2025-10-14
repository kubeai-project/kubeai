package modelproxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	v1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/kubeai-project/kubeai/internal/apiutils"
	"github.com/kubeai-project/kubeai/internal/metrics/metricstest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestHandler(t *testing.T) {
	const (
		model1 = "model1"
		model2 = "model2"

		model3   = "model3"
		adapter3 = "adapter3"

		maxRetries = 3
	)
	models := map[string]testMockModel{
		model1: {},
		model2: {},
		model3: {
			adapters: map[string]bool{
				adapter3: true,
			},
		},
	}

	type metricsTestSpec struct {
		expModel string
	}

	specs := map[string]struct {
		reqBody    string
		reqHeaders map[string]string

		backendPanic bool
		backendCode  int
		backendBody  string

		expRewrittenReqBody    string
		expCode                int
		expBody                string
		expMetrics             *metricsTestSpec
		expBackendRequestCount int
	}{
		"no model": {
			reqBody:                "{}",
			expCode:                http.StatusBadRequest,
			expBody:                `{"error":"bad request: reading model from body: missing 'model' field"}` + "\n",
			expBackendRequestCount: 0,
		},
		"model not found": {
			reqBody:                `{"model":"does-not-exist"}`,
			expCode:                http.StatusNotFound,
			expBody:                fmt.Sprintf(`{"error":%q}`, `model not found: "does-not-exist"`) + "\n",
			expBackendRequestCount: 0,
		},
		"happy 200 model in body": {
			reqBody:     fmt.Sprintf(`{"model":%q,"messages":[]}`, model1),
			backendCode: http.StatusOK,
			backendBody: `{"result":"ok"}`,
			expCode:     http.StatusOK,
			expBody:     `{"result":"ok"}`,
			expMetrics: &metricsTestSpec{
				expModel: model1,
			},
			expBackendRequestCount: 1,
		},
		"happy 200 model+adapter in body": {
			reqBody:             fmt.Sprintf(`{"model":%q,"messages":[]}`, apiutils.MergeModelAdapter(model3, adapter3)),
			expRewrittenReqBody: fmt.Sprintf(`{"model":%q,"messages":[]}`, adapter3),
			backendCode:         http.StatusOK,
			backendBody:         `{"result":"ok"}`,
			expCode:             http.StatusOK,
			expBody:             `{"result":"ok"}`,
			expMetrics: &metricsTestSpec{
				expModel: apiutils.MergeModelAdapter(model3, adapter3),
			},
			expBackendRequestCount: 1,
		},
		"404 model+adapter in body but missing adapter": {
			reqBody: fmt.Sprintf(`{"model":%q,"messages":[]}`, apiutils.MergeModelAdapter(model1, "no-such-adapter")),
			expCode: http.StatusNotFound,
			expBody: fmt.Sprintf(`{"error":%q}`, `model not found: "`+apiutils.MergeModelAdapter(model1, "no-such-adapter")+`"`) + "\n",
		},
		"happy 200 only model in form data": {
			reqHeaders: map[string]string{"Content-Type": "multipart/form-data; boundary=12345"},
			reqBody: fmt.Sprintf(
				"--12345\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\n%s\r\n--12345--\r\n",
				model1,
			),
			// Proxied request should have the model omitted from the body.
			expRewrittenReqBody: "\r\n--12345--\r\n",
			backendCode:         http.StatusOK,
			backendBody:         `{"result":"ok"}`,
			expCode:             http.StatusOK,
			expBody:             `{"result":"ok"}`,
			expMetrics: &metricsTestSpec{
				expModel: model1,
			},
			expBackendRequestCount: 1,
		},
		"happy 200 model with other content in form data": {
			reqHeaders: map[string]string{"Content-Type": "multipart/form-data; boundary=12345"},
			reqBody: fmt.Sprintf(""+
				"--12345\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\n%s\r\n"+
				"--12345\r\nContent-Disposition: form-data; name=\"otherField\"\r\n\r\notherFieldValue\r\n--12345--\r\n",
				model1,
			),
			// Proxied request should have the model omitted from the body.
			expRewrittenReqBody: fmt.Sprintf("" +
				"--12345\r\nContent-Disposition: form-data; name=\"otherField\"\r\n\r\notherFieldValue\r\n--12345--\r\n",
			),
			backendCode: http.StatusOK,
			backendBody: `{"result":"ok"}`,
			expCode:     http.StatusOK,
			expBody:     `{"result":"ok"}`,
			expMetrics: &metricsTestSpec{
				expModel: model1,
			},
			expBackendRequestCount: 1,
		},
		"retryable 500": {
			reqBody:     fmt.Sprintf(`{"model":%q,"messages":[]}`, model1),
			backendCode: http.StatusInternalServerError,
			backendBody: `{"err":"oh no!"}`,
			expCode:     http.StatusInternalServerError,
			expBody:     `{"err":"oh no!"}`,
			expMetrics: &metricsTestSpec{
				expModel: model1,
			},
			expBackendRequestCount: 1 + maxRetries,
		},
		"not retryable 400": {
			reqBody:     fmt.Sprintf(`{"model":%q,"messages":[]}`, model1),
			backendCode: http.StatusBadRequest,
			backendBody: `{"err":"bad request"}`,
			expCode:     http.StatusBadRequest,
			expBody:     `{"err":"bad request"}`,
			expMetrics: &metricsTestSpec{
				expModel: model1,
			},
			expBackendRequestCount: 1,
		},
		"good request but dropped connection": {
			reqBody:      fmt.Sprintf(`{"model":%q,"messages":[]}`, model1),
			backendPanic: true,
			expCode:      http.StatusBadGateway,
			expBody:      `{"error":"Bad Gateway"}` + "\n",
			expMetrics: &metricsTestSpec{
				expModel: model1,
			},
			expBackendRequestCount: 1 + maxRetries,
		},
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			metricstest.Init(t)

			// Mock backend.
			var backendRequestCount int
			sendResponse := make(chan struct{})
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer func() {
					t.Log("waiting for response to be allowed")
					<-sendResponse
					t.Log("sending response")
				}()
				backendRequestCount++

				bdy, err := io.ReadAll(r.Body)
				assert.NoError(t, err, "The request body should be readable")

				if spec.expRewrittenReqBody != "" {
					assert.Equal(t, spec.expRewrittenReqBody, string(bdy), "The rewritten request body should reach the backend")
				} else {
					assert.Equal(t, spec.reqBody, string(bdy), "The exact request body should reach the backend")
				}

				if spec.backendPanic {
					// Panic should close connection.
					// https://pkg.go.dev/net/http#Handler
					panic("panicing on purpose")
				}

				if spec.backendCode != 0 {
					w.WriteHeader(spec.backendCode)
				}
				if spec.backendBody != "" {
					_, _ = w.Write([]byte(spec.backendBody))
				}
			}))

			// Setup handler.
			testInf := &testModelInterface{
				models:  models,
				address: backend.Listener.Addr().String(),
			}
			h := NewHandler(testInf, testInf, maxRetries, nil)
			server := httptest.NewServer(h)

			// Issue request.
			client := &http.Client{}
			req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/chat/completions", strings.NewReader(spec.reqBody))
			require.NoError(t, err)
			for k, v := range spec.reqHeaders {
				req.Header.Add(k, v)
			}

			var resp *http.Response

			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				var err error
				resp, err = client.Do(req)
				require.NoError(t, err, "The client request should not fail")
			}()

			// Assertions based on active requests should go here.
			if spec.expMetrics != nil {
				// Give the metrics some time to be collected.
				time.Sleep(time.Second)

				mets := metricstest.Collect(t)
				metricstest.RequireActiveRequestsMetric(t, mets, spec.expMetrics.expModel, 1)
			}

			close(sendResponse)
			wg.Wait()

			defer resp.Body.Close()
			respBody, err := io.ReadAll(resp.Body)
			require.NoError(t, err)

			// Assert on response.
			assert.Equal(t, spec.expCode, resp.StatusCode, "Unexpected response code to client")
			assert.Equal(t, spec.expBody, string(respBody), "Unexpected response body to client")
			assert.Equal(t, spec.expBackendRequestCount, backendRequestCount, "Unexpected number of requests sent to backend")
			assert.Equal(t, spec.expBackendRequestCount, testInf.hostRequestCount, "Unexpected number of requests for backend hosts")

			// Assert on metrics after the request is responded to.
			if spec.expMetrics != nil {
				mets := metricstest.Collect(t)
				metricstest.RequireActiveRequestsMetric(t, mets, spec.expMetrics.expModel, 0)
			}
		})
	}
}

type testMockModel struct {
	adapters map[string]bool
}

type testModelInterface struct {
	address string

	requestedModel   string
	requestedAdapter string

	hostRequestCount int

	models map[string]testMockModel
}

func (t *testModelInterface) LookupModel(ctx context.Context, model, adapter string, selector []string) (*v1.Model, error) {
	m, ok := t.models[model]
	if ok {
		if adapter == "" {
			return &v1.Model{ObjectMeta: metav1.ObjectMeta{Name: model}}, nil
		}
		if m.adapters == nil {
			return nil, nil
		}
		if m.adapters[adapter] {
			return &v1.Model{ObjectMeta: metav1.ObjectMeta{Name: model}}, nil
		}
	}
	return nil, nil
}

func (t *testModelInterface) ScaleAtLeastOneReplica(ctx context.Context, model string) error {
	return nil
}

func (t *testModelInterface) AwaitBestAddress(ctx context.Context, req *apiutils.Request) (string, func(), error) {
	t.hostRequestCount++
	t.requestedModel = req.Model
	t.requestedAdapter = req.Adapter
	return t.address, func() {}, nil
}
