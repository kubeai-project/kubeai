package integration

import (
	"log"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kubeai-project/kubeai/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestProxy(t *testing.T) {
	sysCfg := baseSysCfg(t)
	sysCfg.ModelAutoscaling.TimeWindow = config.Duration{Duration: 6 * time.Second}
	sysCfg.ModelAutoscaling.Interval = config.Duration{Duration: time.Second}
	initTest(t, sysCfg)

	backendComplete := make(chan struct{})

	t.Cleanup(func() {
		// Finish all requests
		close(backendComplete)
	})

	m := modelForTest(t)
	m.Spec.MaxReplicas = ptr.To[int32](3)
	m.Spec.TargetRequests = ptr.To[int32](1)
	m.Spec.ScaleDownDelaySeconds = ptr.To[int64](1)

	// Create the Model object in the Kubernetes cluster.
	require.NoError(t, testK8sClient.Create(testCtx, m))

	backendRequests := &atomic.Int32{}
	totalBackendRequests := &atomic.Int32{}
	testModelBackend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Println("Serving request from testBackend")
		totalBackendRequests.Add(1)
		backendRequests.Add(1)
		defer backendRequests.Add(-1)
		log.Println("Added request to backend:", backendRequests.Load())
		<-backendComplete
		log.Println("Sending response from backend")
		w.WriteHeader(200)
	}))

	updateModelWithBackend(t, m, testModelBackend)

	// Wait for controller cache to sync.
	time.Sleep(3 * time.Second)

	var wg sync.WaitGroup

	// Send request number 1
	sendRequests(t, &wg, m.Name, nil, 1, http.StatusOK, "", "request 1")

	requireModelReplicas(t, m, 1, "Replicas should be scaled up to 1 to process messaging request", 5*time.Second)
	requireModelPods(t, m, 1, "Pod should be created for the messaging request", 5*time.Second)
	markAllModelPodsReady(t, m)
	closeChannels(backendComplete, 1)
	require.Equal(t, int32(1), totalBackendRequests.Load(), "ensure the request made its way to the backend")

	const autoscaleUpWait = 25 * time.Second
	// Ensure the deployment is autoscaled past 1.
	// Simulate the backend processing the request.
	sendRequests(t, &wg, m.Name, nil, 2, http.StatusOK, "", "request 2,3")
	requireModelReplicas(t, m, 2, "Replicas should be scaled up to 2 to process pending messaging request", autoscaleUpWait)
	requireModelPods(t, m, 2, "2 Pods should be created for the messaging requests", 5*time.Second)
	markAllModelPodsReady(t, m)

	// Make sure deployment will not be scaled past max (3).
	sendRequests(t, &wg, m.Name, nil, 2, http.StatusOK, "", "request 4,5")
	require.Never(t, func() bool {
		assert.NoError(t, testK8sClient.Get(testCtx, client.ObjectKeyFromObject(m), m))
		return *m.Spec.Replicas > *m.Spec.MaxReplicas
	}, autoscaleUpWait, time.Second/10, "Replicas should not be scaled past MaxReplicas")

	closeChannels(backendComplete, 4)
	require.Equal(t, int32(5), totalBackendRequests.Load(), "ensure all the requests made their way to the backend")

	// Ensure the deployment is autoscaled back down to MinReplicas.
	const autoscaleDownWait = 25 * time.Second
	requireModelReplicas(t, m, m.Spec.MinReplicas, "Replicas should scale back to MinReplicas", autoscaleDownWait)
	requireModelPods(t, m, int(m.Spec.MinReplicas), "Pods should be removed", 5*time.Second)

	t.Log("Waiting for all requests to complete")
	wg.Wait()
	t.Log("All requests completed")
}
