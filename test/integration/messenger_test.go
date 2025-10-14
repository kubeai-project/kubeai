package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kubeai-project/kubeai/internal/config"
	"github.com/stretchr/testify/require"
	"gocloud.dev/pubsub"
)

// TestMessenger tests the messenger integration using an in-memory pubsub implementation.
// The test spins up a test backend server that emulates the expected behavior of a model Pod
// (NOTE: Pod containers are never actually run in integration tests).
func TestMessenger(t *testing.T) {
	var err error
	testRequestsTopic, err = pubsub.OpenTopic(testCtx, memRequestsURL)
	require.NoError(t, err)

	sysCfg := baseSysCfg(t)
	sysCfg.Messaging = config.Messaging{
		Streams: []config.MessageStream{
			{
				RequestsURL:  memRequestsURL,
				ResponsesURL: memResponsesURL,
			},
		},
	}
	initTest(t, sysCfg)

	time.Sleep(3 * time.Second)
	testResponsesSubscription, err = pubsub.OpenSubscription(testCtx, memResponsesURL)
	require.NoError(t, err)

	m := modelForTest(t)
	require.NoError(t, testK8sClient.Create(testCtx, m))

	t.Logf("Giving the messenger time to start")
	time.Sleep(3 * time.Second)

	backendComplete := make(chan struct{})
	testModelBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Ignore non-POST requests (i.e. metrics requests from autoscaler).
		if r.Method != http.MethodPost {
			t.Logf("Received non-POST request: %s %s", r.Method, r.URL)
			return
		}
		t.Logf("Serving request from testBackend")

		var reqBody struct {
			Model string `json:"model"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&reqBody))
		require.Equal(t, m.Name, reqBody.Model)

		<-backendComplete

		w.Write([]byte(fmt.Sprintf(`{"model": %q, "choices": [{"text": "hey"}]}`, reqBody.Model)))
	}))

	updateModelWithBackend(t, m, testModelBackend)

	// Wait for controller cache to sync.
	time.Sleep(3 * time.Second)

	// Send request id "a"
	sendRequestMessage(t, "/v1/completions", m.Name, "a")

	// Assert on replicas before completing the request - otherwise there is a race condition
	// with the autoscaler.
	requireModelReplicas(t, m, 1, "Replicas should be scaled up to 1 to process messaging request", 5*time.Second)
	requireModelPods(t, m, 1, "Pod should be created for the messaging request", 5*time.Second)
	markAllModelPodsReady(t, m)
	completeBackendRequests(backendComplete, 1)

	shouldReceiveResponseMessage(t, m.Name, "a")

	sendRequestMessage(t, "/v1/completions", "non-existant-model", "b")
	shouldReceiveResponseErrMessage(t, http.StatusNotFound, "model not found: \"non-existant-model\"", "b")
}

func shouldReceiveResponseErrMessage(t *testing.T, statusCode int, message string, id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	t.Logf("Waiting for response error message: id %q", id)
	resp, err := testResponsesSubscription.Receive(ctx)
	require.NoError(t, err)
	resp.Ack()

	require.JSONEq(t, fmt.Sprintf(`
{
	"metadata": {"my-id": %q},
	"status_code": %d,
	"body": {
		"error": {
			"message": %q
		}
	}
}`, id, statusCode, message), string(resp.Body))
}

func shouldReceiveResponseMessage(t *testing.T, model, id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	t.Logf("Waiting for response message for model %q and id %q", model, id)
	resp, err := testResponsesSubscription.Receive(ctx)
	require.NoError(t, err)
	resp.Ack()
	require.JSONEq(t, fmt.Sprintf(`
{
	"metadata": {"my-id": %q},
	"status_code": 200,
	"body": {
		"model": %q,
		"choices": [
			{"text": "hey"}
		]
	}
}`, id, model), string(resp.Body))
}

func sendRequestMessage(t *testing.T, path, modelName, id string) {
	body := []byte(fmt.Sprintf(`
{
	"path": %q,
	"body": {
		"model": %q
	},
	"metadata": {"my-id": %q}
}`,
		path, modelName, id))

	err := testRequestsTopic.Send(context.Background(), &pubsub.Message{
		Body: body,
	})
	require.NoError(t, err)
}
