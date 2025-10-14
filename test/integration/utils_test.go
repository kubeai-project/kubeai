package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	v1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/kubeai-project/kubeai/internal/openaiserver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

func modelLabelSelectorForTest(t *testing.T) string {
	return fmt.Sprintf("test-case-name=%s", strings.ToLower(t.Name()))
}

func modelForTest(t *testing.T) *v1.Model {
	m := &v1.Model{
		ObjectMeta: metav1.ObjectMeta{
			Name:      strings.ToLower(t.Name()),
			Namespace: testNS,
			Annotations: map[string]string{
				"test-annotation": "test",
			},
			Labels: map[string]string{
				"test-label":     "test",
				"test-case-name": strings.ToLower(t.Name()),
			},
		},
		Spec: v1.ModelSpec{
			Owner:           "test",
			URL:             "hf://test-org/test-model",
			Features:        []v1.ModelFeature{v1.ModelFeatureTextGeneration},
			Engine:          v1.VLLMEngine,
			ResourceProfile: resourceProfileCPU + ":3",
			// Should default to "default".
			//AutoscalingProfile: "default",
			Args: []string{"--test-arg"},
			Env:  map[string]string{"TEST_ENV": "test"},
		},
	}
	t.Cleanup(func() {
		err := testK8sClient.Delete(testCtx, m)
		if err != nil {
			t.Logf("Cleanup: deleting Model: %v", err)
		}
	})
	return m
}

func updateModel(t *testing.T, m *v1.Model, modify func(), msg string) {
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		assert.NoError(t, testK8sClient.Get(testCtx, client.ObjectKeyFromObject(m), m))
		modify()
		assert.NoError(t, testK8sClient.Update(testCtx, m))
	}, 2*time.Second, time.Second/10, "Updating Model should succeed: "+msg)
}

func updateAllModelPods(t *testing.T, m *v1.Model, modify func(*corev1.Pod) bool, mustModifyN int, msg string) {
	var modified int
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		podList := &corev1.PodList{}
		if !assert.NoError(t, testK8sClient.List(testCtx, podList, client.MatchingLabels{"model": m.Name})) {
			return
		}
		for i := range podList.Items {
			if modify(&podList.Items[i]) {
				if !assert.NoError(t, testK8sClient.Update(testCtx, &podList.Items[i])) {
					return
				}
				modified++
			}
		}
	}, 2*time.Second, time.Second/10, "Updating all model Pods should succeed: "+msg)
	require.Equal(t, mustModifyN, modified, "Number of Pods modified should match")
}

func requireModelReplicas(t *testing.T, m *v1.Model, expectedReplicas int32, msg string, after time.Duration) {
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		if !assert.NoError(t, testK8sClient.Get(testCtx, client.ObjectKeyFromObject(m), m)) {
			return
		}
		//jsn, _ := json.MarshalIndent(m, "", "  ")
		//fmt.Println(string(jsn))
		if !assert.NotNil(t, m.Spec.Replicas) {
			return
		}
		assert.Equal(t, expectedReplicas, *m.Spec.Replicas)
	}, after, time.Second/10, "Model Replicas should match: "+msg)
}

func requireModelPods(t *testing.T, m *v1.Model, expectedPods int, msg string, after time.Duration) {
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		podList := &corev1.PodList{}
		if !assert.NoError(t, testK8sClient.List(testCtx, podList, client.InNamespace(testNS), client.MatchingLabels{"model": m.Name})) {
			return
		}
		if !assert.Len(t, podList.Items, expectedPods) {
			return
		}
	}, after, time.Second/10, "Model Pods should match: "+msg)
}

func markAllModelPodsReady(t *testing.T, m *v1.Model) {
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		podList := &corev1.PodList{}
		if !assert.NoError(t, testK8sClient.List(testCtx, podList, client.InNamespace(testNS), client.MatchingLabels{"model": m.Name})) {
			return
		}
		for _, pod := range podList.Items {
			pod.Status.Phase = corev1.PodRunning
			pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
			if !assert.NoError(t, testK8sClient.Status().Update(testCtx, &pod)) {
				return
			}
		}
	}, 2*time.Second, time.Second/10, "All model Pods should be marked ready")
}

func completeBackendRequests(c chan struct{}, n int) {
	for i := 0; i < n; i++ {
		c <- struct{}{}
	}
}

func mustFindPodContainerByName(t assert.TestingT, pod *corev1.Pod, name string) corev1.Container {
	for _, c := range pod.Spec.Containers {
		if c.Name == name {
			return c
		}
	}
	assert.Fail(t, "Container not found: "+name)
	return corev1.Container{}
}

func updateModelWithBackend(t *testing.T, m *v1.Model, testModelBackend *httptest.Server) {
	t.Logf("testBackend URL: %s", testModelBackend.URL)
	u, err := url.Parse(testModelBackend.URL)
	require.NoError(t, err)

	updateModel(t, m, func() {
		m.ObjectMeta.Annotations[v1.ModelPodIPAnnotation] = u.Hostname()
		m.ObjectMeta.Annotations[v1.ModelPodPortAnnotation] = u.Port()
	}, "Set model IP/port annotations to direct requests to testBackend instead of the Pod's (non-existant) IP")
}

func sendRequests(t *testing.T, wg *sync.WaitGroup, modelName string, selectorHeaders []string, n int, expCode int, expBodyContains string, msg string) {
	t.Helper()
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sendOpenAIInferenceRequest(t, modelName, selectorHeaders, expCode, expBodyContains, msg)
		}()
	}
}

func sendOpenAIInferenceRequest(t *testing.T, modelName string, selectorHeaders []string, expCode int, expBodyContains string, msg string) {
	t.Helper()
	body := []byte(fmt.Sprintf(`{"model": %q}`, modelName))
	req, err := http.NewRequest(http.MethodPost, "http://localhost:8000/openai/v1/completions", bytes.NewReader(body))
	if t.Failed() {
		// Don't report errors if the test already failed - confusing.
		return
	}
	require.NoError(t, err, msg)
	for _, selector := range selectorHeaders {
		t.Logf("Using selector: %s", selector)
		req.Header.Add("X-Label-Selector", selector)
	}

	res, err := testHTTPClient.Do(req)
	if t.Failed() {
		// Don't report errors if the test already failed - confusing.
		return
	}
	require.NoError(t, err, msg)
	defer res.Body.Close()
	require.Equal(t, expCode, res.StatusCode, msg)

	if expBodyContains != "" {
		bdy, err := io.ReadAll(res.Body)
		require.NoError(t, err, msg)
		require.Contains(t, string(bdy), expBodyContains, msg)
	}
}

func requireOpenAIModelList(t *testing.T, selectorHeaders []string, expIDs []string, msg string) {
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		//t.Helper()
		req, err := http.NewRequest(http.MethodGet, "http://localhost:8000/openai/v1/models", nil)
		if !assert.NoError(t, err, msg) {
			return
		}
		for _, selector := range selectorHeaders {
			req.Header.Add("X-Label-Selector", selector)
		}

		res, err := testHTTPClient.Do(req)
		if !assert.NoError(t, err, msg) {
			return
		}
		if !assert.Equal(t, http.StatusOK, res.StatusCode, msg) {
			return
		}
		defer res.Body.Close()

		var respBody struct {
			Data []openaiserver.Model `json:"data"`
		}
		if !assert.NoError(t, json.NewDecoder(res.Body).Decode(&respBody), msg) {
			return
		}

		ids := make([]string, len(respBody.Data))
		for i, m := range respBody.Data {
			ids[i] = m.ID
		}

		assert.ElementsMatch(t, expIDs, ids)
	}, 5*time.Second, time.Second/10, msg)
}

func closeChannels(c chan struct{}, n int) {
	for i := 0; i < n; i++ {
		c <- struct{}{}
	}
}

func requireUpdateJobAsCompleted(t *testing.T, job *batchv1.Job) {
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		setJobCompletedStatus(job)
		if !assert.NoError(t, testK8sClient.Status().Update(testCtx, job)) {
			assert.NoError(t, testK8sClient.Get(testCtx, client.ObjectKeyFromObject(job), job))
		}
	}, 2*time.Second, time.Second/10)
}

func setJobCompletedStatus(job *batchv1.Job) {
	job.Status.Succeeded = *job.Spec.Completions
	currentTime := time.Now()
	job.Status.StartTime = &metav1.Time{Time: currentTime.Add(-time.Minute * 1)}
	job.Status.CompletionTime = &metav1.Time{Time: currentTime}
	for i := range job.Status.Conditions {
		if job.Status.Conditions[i].Type == batchv1.JobComplete {
			job.Status.Conditions[i].Status = corev1.ConditionTrue
			return
		}
	}
	job.Status.Conditions = append(job.Status.Conditions,
		batchv1.JobCondition{
			Type:   batchv1.JobSuccessCriteriaMet,
			Status: corev1.ConditionTrue,
		},
		batchv1.JobCondition{
			Type:   batchv1.JobComplete,
			Status: corev1.ConditionTrue,
		})
}

// logPods is useful for debugging why a test case is failing.
func logPods(t *testing.T) {
	podList := &corev1.PodList{}
	require.NoError(t, testK8sClient.List(testCtx, podList, client.InNamespace(testNS)))
	fmt.Println("=== Pods ===")
	for _, pod := range podList.Items {
		yml, err := yaml.Marshal(pod)
		require.NoError(t, err)
		fmt.Println(string(yml))
		fmt.Println("---")
	}
}
