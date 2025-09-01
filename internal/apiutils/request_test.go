package apiutils

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	v1 "github.com/substratusai/kubeai/api/k8s/v1"
)

func TestParseRequest(t *testing.T) {
	cases := []struct {
		name          string
		body          string
		path          string
		headers       http.Header
		expModel      string
		expAdapter    string
		expPrefix     string
		expRoutingKey string
	}{
		{
			name:     "model only",
			body:     `{"model": "test-model"}`,
			path:     "/v1/chat/completions",
			expModel: "test-model",
		},
		{
			name:       "model and adapter",
			body:       `{"model": "test-model_test-adapter"}`,
			path:       "/v1/chat/completions",
			expModel:   "test-model",
			expAdapter: "test-adapter",
		},
		{
			name:     "openai chat completion missing messages",
			body:     `{"model": "test-model"}`,
			path:     "/v1/chat/completions",
			expModel: "test-model",
		},
		{
			name:     "openai chat completion missing user message",
			body:     `{"model": "test-model", "messages": [{"role": "system", "content": "test"}]}`,
			path:     "/v1/chat/completions",
			expModel: "test-model",
		},
		{
			name:      "openai chat completion",
			body:      `{"model": "test-model", "messages": [{"role": "user", "content": "test-prefix"}]}`,
			path:      "/v1/chat/completions",
			expModel:  "test-model",
			expPrefix: "test-prefi", // "test-prefix" (max 10) --> "test-prefi"
		},
		{
			name:      "openai legacy completion",
			body:      `{"model": "test-model", "prompt": "test-prefix"}`,
			path:      "/v1/completions",
			expModel:  "test-model",
			expPrefix: "test-prefi", // "test-prefix" (max 10) --> "test-prefi"
		},
		{
			name:          "routing key header",
			body:          `{"model": "test-model"}`,
			path:          "/v1/chat/completions",
			headers:       http.Header{"Routing-Key": []string{"my-routing-key"}},
			expModel:      "test-model",
			expRoutingKey: "my-routing-key",
		},
		{
			name: "routing key header case insensitive",
			body: `{"model": "test-model"}`,
			path: "/v1/chat/completions",
			headers: func() http.Header {
				h := make(http.Header)
				h.Set("routing-key", "case-insensitive-key")
				return h
			}(),
			expModel:      "test-model",
			expRoutingKey: "case-insensitive-key",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()

			mockClient := &mockModelClient{prefixCharLen: 10}

			req, err := ParseRequest(ctx, mockClient, bytes.NewReader([]byte(c.body)), c.path, c.headers)
			require.NoError(t, err)

			require.Equal(t, c.expModel, req.Model, "model")
			require.Equal(t, c.expAdapter, req.Adapter, "adapter")
			require.Equal(t, c.expPrefix, req.Prefix, "prefix")
			require.Equal(t, c.expRoutingKey, req.RoutingKey, "routing key")
		})
	}

}

type mockModelClient struct {
	prefixCharLen int
}

func (m *mockModelClient) LookupModel(ctx context.Context, model, adapter string, selectors []string) (*v1.Model, error) {
	return &v1.Model{
		Spec: v1.ModelSpec{
			LoadBalancing: v1.LoadBalancing{
				Strategy: v1.PrefixHashStrategy,
				PrefixHash: v1.PrefixHash{
					// "test-prefix" --> "test-prefi"
					PrefixCharLength: m.prefixCharLen,
				},
			},
		},
	}, nil
}
