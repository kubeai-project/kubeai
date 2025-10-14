package integration

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kubeai-project/kubeai/internal/config"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"
)

// TestAutoscalingHA tests autoscaling when there are multiple KubeAI instances.
// Metrics are mocked using test servers.
func TestAutoscalingHA(t *testing.T) {
	m := modelForTest(t)
	m.Spec.MaxReplicas = ptr.To[int32](100)
	m.Spec.TargetRequests = ptr.To[int32](1)
	m.Spec.ScaleDownDelaySeconds = ptr.To[int64](2)

	s1 := newTestMetricsServer(t, m.Name)
	s2 := newTestMetricsServer(t, m.Name)
	s3 := newTestMetricsServer(t, m.Name)

	sysCfg := baseSysCfg(t)
	sysCfg.ModelAutoscaling.TimeWindow = config.Duration{Duration: 1 * time.Second}
	sysCfg.ModelAutoscaling.Interval = config.Duration{Duration: time.Second / 4}
	sysCfg.FixedSelfMetricAddrs = []string{
		s1.addr(),
		s2.addr(),
		s3.addr(),
	}
	t.Logf("FixedSelfMetricAddrs: %v", sysCfg.FixedSelfMetricAddrs)
	initTest(t, sysCfg)

	s1.activeRequests.Store(1)
	s2.activeRequests.Store(2)
	s3.activeRequests.Store(3)

	// Create the Model object in the Kubernetes cluster.
	require.NoError(t, testK8sClient.Create(testCtx, m))

	// 1 + 2 + 3 = 6
	requireModelReplicas(t, m, 6, "Replicas should be autoscaled", 15*time.Second)

	s1.activeRequests.Store(0)
	s2.activeRequests.Store(0)
	s3.activeRequests.Store(0)

	requireModelReplicas(t, m, 0, "Replicas should be autoscaled to zero", 15*time.Second)
}

func newTestMetricsServer(t *testing.T, model string) *testMetricsServer {
	s := &testMetricsServer{
		t:              t,
		model:          model,
		activeRequests: &atomic.Int64{},
	}
	s.Server = httptest.NewServer(s)
	t.Cleanup(s.Close)
	return s
}

type testMetricsServer struct {
	t *testing.T
	*httptest.Server
	model          string
	activeRequests *atomic.Int64
}

func (s *testMetricsServer) addr() string {
	return s.Server.Listener.Addr().String()
}

func (s *testMetricsServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	require.Equal(s.t, "GET", r.Method)
	require.Equal(s.t, "/metrics", r.URL.Path)

	const format = `
# HELP kubeai_inference_requests_active The number of active requests by model
# TYPE kubeai_inference_requests_active gauge
kubeai_inference_requests_active{otel_scope_name="kubeai.org",otel_scope_version="",request_model="%s",request_type="http"} %d
`
	promMetrics := fmt.Sprintf(format, s.model, s.activeRequests.Load())
	_, err := w.Write([]byte(promMetrics))
	require.NoError(s.t, err)
}
