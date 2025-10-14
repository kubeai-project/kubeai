package apiutils

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	v1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/stretchr/testify/require"
)

func TestParseRequest(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		path       string
		headers    http.Header
		expModel   string
		expAdapter string
		expPrefix  string
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
			name:     "rerank request",
			body:     `{"model": "test-model", "query": "q", "documents": ["d1", "d2"]}`,
			path:     "/v1/rerank",
			expModel: "test-model",
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
