package integration

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestScaleUpAndDown(t *testing.T) {
	const modelName = "test-model-a"
	deploy := testDeployment(modelName)

	require.NoError(t, testK8sClient.Create(testCtx, deploy))

	backendComplete := make(chan struct{})

	backendRequests := &atomic.Int32{}
	testBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Println("Serving request from testBackend")
		backendRequests.Add(1)
		<-backendComplete
		w.WriteHeader(200)
	}))

	// Mock an EndpointSlice.
	testBackendURL, err := url.Parse(testBackend.URL)
	require.NoError(t, err)
	testBackendPort, err := strconv.Atoi(testBackendURL.Port())
	require.NoError(t, err)
	require.NoError(t, testK8sClient.Create(testCtx,
		endpointSlice(
			modelName,
			testBackendURL.Hostname(),
			int32(testBackendPort),
		),
	))

	// Wait for deployment mapping to sync.
	time.Sleep(3 * time.Second)

	// Send request number 1
	var wg sync.WaitGroup
	sendRequests(t, &wg, modelName, 1, http.StatusOK)

	requireDeploymentReplicas(t, deploy, 1)
	require.Equal(t, int32(1), backendRequests.Load(), "ensure the request made its way to the backend")
	completeRequests(backendComplete, 1)

	// Ensure the deployment scaled scaled past 1.
	// 1/2 should be admitted
	// 1/2 should remain in queue
	sendRequests(t, &wg, modelName, 2, http.StatusOK)
	requireDeploymentReplicas(t, deploy, 2)

	// Make sure deployment will not be scaled past default max (3).
	sendRequests(t, &wg, modelName, 2, http.StatusOK)
	requireDeploymentReplicas(t, deploy, 3)

	// Have the mock backend respond to the remaining 4 requests.
	completeRequests(backendComplete, 4)

	// Ensure scale-down.
	requireDeploymentReplicas(t, deploy, 0)

	t.Logf("Waiting for wait group")
	wg.Wait()
}

func TestHandleModelUndeployment(t *testing.T) {
	const modelName = "test-model-b"
	deploy := testDeployment(modelName)

	require.NoError(t, testK8sClient.Create(testCtx, deploy))

	backendComplete := make(chan struct{})

	backendRequests := &atomic.Int32{}
	testBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Println("Serving request from testBackend")
		backendRequests.Add(1)
		<-backendComplete
		w.WriteHeader(200)
	}))

	mockEndpointSlice(t, modelName, testBackend)

	// Wait for deployment mapping to sync.
	time.Sleep(3 * time.Second)

	// Send request number 1
	var wg sync.WaitGroup
	// send single request to scale up and block on the handler to build a queue
	sendRequests(t, &wg, modelName, 1, http.StatusOK)

	requireDeploymentReplicas(t, deploy, 1)
	require.Equal(t, int32(1), backendRequests.Load(), "ensure the request made its way to the backend")
	// Add some more requests to the queue but with 404 expected
	// because the deployment is deleted before un-queued
	sendRequests(t, &wg, modelName, 2, http.StatusNotFound)

	require.NoError(t, testK8sClient.Delete(testCtx, deploy))

	// Check that the deployment was deleted
	err := testK8sClient.Get(testCtx, client.ObjectKey{
		Namespace: deploy.Namespace,
		Name:      deploy.Name,
	}, deploy)

	// ErrNotFound is desired since we delete the resource earlier
	assert.True(t, apierrors.IsNotFound(err))
	// release blocked request
	completeRequests(backendComplete, 1)

	// Wait for deployment mapping to sync.
	require.Eventually(t, func() bool {
		return queueManager.TotalCounts()[modelName+"-deploy"] == 0
	}, 3*time.Second, 100*time.Millisecond)

	t.Logf("Waiting for wait group")
	wg.Wait()
}

func sendRequests(t *testing.T, wg *sync.WaitGroup, modelName string, n int, expCode int) {
	for i := 0; i < n; i++ {
		sendRequest(t, wg, modelName, expCode)
	}
}

func sendRequest(t *testing.T, wg *sync.WaitGroup, modelName string, expCode int) {
	t.Helper()
	wg.Add(1)
	go func() {
		defer wg.Done()

		body := []byte(fmt.Sprintf(`{"model": %q}`, modelName))
		req, err := http.NewRequest(http.MethodPost, testServer.URL, bytes.NewReader(body))
		requireNoError(err)

		res, err := testHTTPClient.Do(req)
		require.NoError(t, err)
		require.Equal(t, expCode, res.StatusCode)
	}()
}

func completeRequests(c chan struct{}, n int) {
	for i := 0; i < n; i++ {
		c <- struct{}{}
	}
}

func testDeployment(name string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-deploy",
			Namespace: testNamespace,
			Labels: map[string]string{
				"app": name,
			},
			Annotations: map[string]string{
				"lingo.substratus.ai/models": name,
			},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "model",
							Image: "some-model:1.2.3",
						},
					},
				},
			},
		},
	}
}
