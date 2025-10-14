package integration

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	v1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/kubeai-project/kubeai/internal/apiutils"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestAdapters(t *testing.T) {
	sysCfg := baseSysCfg(t)
	initTest(t, sysCfg)
	m := modelForTest(t)
	const (
		adapter1 = "adapter1"
		adapter2 = "adapter2"
	)
	m.Spec.Adapters = []v1.Adapter{
		{Name: adapter1, URL: "hf://test-repo/test-adapter"},
		{Name: adapter2, URL: "s3://test-bucket/test-path"},
	}
	require.NoError(t, testK8sClient.Create(testCtx, m))

	totalBackendRequests := &atomic.Int32{}
	testModelBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		totalBackendRequests.Add(1)
		w.WriteHeader(200)
	}))
	updateModelWithBackend(t, m, testModelBackend)

	// NOTE: Update to 1 min replicas needs to happen after the Model is updated with the backend because
	// updates to annotations will not be propogated to Pods if the Model is updated after the Pods are created.
	updateModel(t, m, func() {
		m.Spec.MinReplicas = 1
	}, "Set min replicas to 1")

	// Mark Pods as having adapter already loaded.

	requireModelPods(t, m, 1, "Pod should be created", 5*time.Second)
	updateAllModelPods(t, m, func(p *corev1.Pod) bool {
		if _, ok := p.ObjectMeta.Labels[v1.PodAdapterLabel(adapter1)]; !ok {
			p.ObjectMeta.Labels[v1.PodAdapterLabel(adapter1)] = "some-hash"
			return true
		}
		return false
	}, 1, "Update model with adapter1 hash to simulate adapter loaded")
	markAllModelPodsReady(t, m)

	// Send OpenAI-API requests.

	selectors := []string{modelLabelSelectorForTest(t)}

	// logPods(t)

	requireOpenAIModelList(t, selectors, []string{
		m.Name,
		apiutils.MergeModelAdapter(m.Name, adapter1),
		apiutils.MergeModelAdapter(m.Name, adapter2),
	}, "Model list should contain the model and its adapters")

	sendOpenAIInferenceRequest(t, m.Name, selectors, http.StatusOK, "", "inference request 1")
	sendOpenAIInferenceRequest(t, apiutils.MergeModelAdapter(m.Name, adapter1), selectors, http.StatusOK, "", "inference request 2 to adapter1")
	require.Equal(t, int32(2), totalBackendRequests.Load(), "Adapter should not be loaded yet")
}
