package apiutils

import (
	"context"
	"net/http"
	"strings"
	"testing"

	k8sv1 "github.com/kubeai-project/kubeai/api/k8s/v1"

	"github.com/stretchr/testify/require"
)

type messagesModelClient struct{ mockModelClient }

func (m *messagesModelClient) LookupModel(ctx context.Context, model, adapter string, selectors []string) (*k8sv1.Model, error) {
	result, err := m.mockModelClient.LookupModel(ctx, model, adapter, selectors)
	result.Spec.Features = []k8sv1.ModelFeature{k8sv1.ModelFeatureTextGeneration}
	return result, err
}

func TestMessagesRoutingEnvelope(t *testing.T) {
	for _, tc := range []struct{ name, body, model, adapter, prefix string }{
		{"plain", ` {"model":"chat","messages":[{"role":"user","content":"hello world"}],"tools":[{"input_schema":{"z":1,"a":2}}],"thinking":{"type":"enabled"},"extension":true} `, "chat", "", "hello"},
		{"adapter", `{"model":"chat_adapter","messages":[{"role":"user","content":[{"type":"text","text":"hello world"}]}],"extension":{"z":1,"a":2}}`, "chat", "adapter", "hello"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The request tests' mock has no feature; use a wrapper to enable generation.
			client := &messagesModelClient{mockModelClient{prefixCharLen: 5}}
			request, err := ParseRequest(context.Background(), client, strings.NewReader(tc.body), MessagesPath, http.Header{})
			require.NoError(t, err)
			require.Equal(t, tc.model, request.Model)
			require.Equal(t, tc.adapter, request.Adapter)
			require.Equal(t, tc.prefix, request.Prefix)
			require.EqualValues(t, len(request.Body), request.ContentLength)
			if tc.adapter == "" {
				require.Equal(t, tc.body, string(request.Body))
			} else {
				require.JSONEq(t, strings.Replace(tc.body, "chat_adapter", "adapter", 1), string(request.Body))
			}
		})
	}
}

func TestMessagesBodyLimit(t *testing.T) {
	_, err := ParseRequest(context.Background(), &messagesModelClient{}, strings.NewReader(strings.Repeat(" ", int(MaxMessagesBodyBytes)+1)), MessagesPath, nil)
	require.ErrorIs(t, err, ErrRequestTooLarge)
}
