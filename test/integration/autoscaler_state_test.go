package integration

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/kubeai-project/kubeai/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

// TestAutoscalerState tests the autoscaler's state management.
func TestAutoscalerState(t *testing.T) {
	m := modelForTest(t)
	m.Spec.MaxReplicas = ptr.To[int32](100)
	m.Spec.TargetRequests = ptr.To[int32](1)
	m.Spec.ScaleDownDelaySeconds = ptr.To[int64](2)

	ms := newTestMetricsServer(t, m.Name)

	sysCfg := baseSysCfg(t)
	sysCfg.ModelAutoscaling.TimeWindow = config.Duration{Duration: 1 * time.Second}
	sysCfg.ModelAutoscaling.Interval = config.Duration{Duration: time.Second / 4}
	sysCfg.FixedSelfMetricAddrs = []string{
		ms.addr(),
	}
	t.Logf("FixedSelfMetricAddrs: %v", sysCfg.FixedSelfMetricAddrs)
	initTest(t, sysCfg)

	ms.activeRequests.Store(2)

	// Create the Model object in the Kubernetes cluster.
	require.NoError(t, testK8sClient.Create(testCtx, m))

	requireModelReplicas(t, m, 2, "Replicas should be autoscaled", 15*time.Second)

	// Assert that state was saved.
	cm := &corev1.ConfigMap{}
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := testK8sClient.Get(testCtx, types.NamespacedName{Name: sysCfg.ModelAutoscaling.StateConfigMapName, Namespace: testNS}, cm)
		if !assert.NoError(t, err) {
			return
		}
		if !assert.NotNil(t, cm.Data) {
			return
		}
		data, ok := cm.Data["models"]
		if !assert.True(t, ok) {
			return
		}
		type modelState struct {
			AverageActiveRequests float64 `json:"averageActiveRequests"`
		}
		var state struct {
			Models map[string]modelState `json:"models"`
		}
		if !assert.NoError(t, json.Unmarshal([]byte(data), &state)) {
			return
		}
		if !assert.NotNil(t, state.Models) {
			return
		}
		assert.Equal(t, state.Models[m.Name], modelState{
			AverageActiveRequests: 2.0,
		})
	}, 15*time.Second, 1*time.Second)
}
