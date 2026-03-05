package integration

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLWSMultiNode(t *testing.T) {
	sysCfg := baseSysCfg(t)
	initTest(t, sysCfg)

	m := modelForTest(t)
	// 3-part resource profile triggers LWS mode: <name>:<tp>:<pp>
	m.Spec.ResourceProfile = resourceProfileCPU + ":1:2"
	require.NoError(t, testK8sClient.Create(testCtx, m))

	totalBackendRequests := &atomic.Int32{}
	testModelBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		totalBackendRequests.Add(1)
		w.WriteHeader(200)
	}))
	updateModelWithBackend(t, m, testModelBackend)

	// Set min replicas to 1 to trigger LWS creation.
	updateModel(t, m, func() {
		m.Spec.MinReplicas = 1
	}, "Set min replicas to 1")

	// Since we are in envtest, the LWS controller is not running so
	// the LWS won't actually create pods. But we can verify the LWS object
	// was created by the model controller.
	// Mark all head pods ready so the proxy can route to them.
	markAllModelPodsReady(t, m)

	// Send an inference request — it should reach our mock backend
	// via the head pod address.
	selectors := []string{modelLabelSelectorForTest(t)}
	sendOpenAIInferenceRequest(t, m.Name, selectors, http.StatusOK, "", "inference request 1")
	require.Equal(t, int32(1), totalBackendRequests.Load(), "Backend should have received 1 request")
}
