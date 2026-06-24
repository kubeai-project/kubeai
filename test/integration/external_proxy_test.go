package integration

import (
	"net/http"
	"testing"
	"time"

	"github.com/kubeai-project/kubeai/internal/config"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

func TestExternalMode(t *testing.T) {
	sysCfg := baseSysCfg(t)
	sysCfg.Proxy.Mode = config.ProxyModeExternal
	initTest(t, sysCfg)

	t.Run("ProxyNotStarted", func(t *testing.T) {
		// Attempt to request the proxy endpoint on :8000
		// We expect this to fail with connection refused since proxy isn't started
		client := &http.Client{Timeout: 1 * time.Second}
		_, err := client.Get("http://localhost:8000/")
		require.Error(t, err)
		require.Contains(t, err.Error(), "refused")
	})

	t.Run("HeadlessServiceCreatedAndDeleted", func(t *testing.T) {
		m := modelForTest(t)
		m.Name = "test-external-model"
		m.Labels["test-case-name"] = "test-external-model"
		m.Spec.MaxReplicas = ptr.To[int32](1)
		require.NoError(t, testK8sClient.Create(testCtx, m))

		// Wait for the headless service to be created by the model controller
		svcName := ""
		var svc corev1.Service
		require.Eventually(t, func() bool {
			// Because UID is generated upon creation, we must fetch the model first to get the UID
			if err := testK8sClient.Get(testCtx, types.NamespacedName{Name: m.Name, Namespace: m.Namespace}, m); err != nil {
				return false
			}
			svcName = m.Name + "-" + string(m.UID)[:6]
			err := testK8sClient.Get(testCtx, types.NamespacedName{Name: svcName, Namespace: m.Namespace}, &svc)
			return err == nil
		}, 10*time.Second, 100*time.Millisecond)

		require.Equal(t, corev1.ClusterIPNone, svc.Spec.ClusterIP)
		require.Equal(t, m.Name, svc.Spec.Selector["kubeai.org/model"])
		require.Equal(t, string(m.UID), svc.Spec.Selector["kubeai.org/model-uid"])

		// Delete the model and expect Service to be deleted by GC
		// (In envtest GC doesn't run automatically, but we can verify it is owned by the model)
		require.Equal(t, 1, len(svc.OwnerReferences))
		require.Equal(t, m.Name, svc.OwnerReferences[0].Name)

		require.NoError(t, testK8sClient.Delete(testCtx, m))
	})

	t.Run("MinReplicasZeroWarns", func(t *testing.T) {
		m := modelForTest(t)
		m.Name = "test-min-replicas-zero"
		m.Labels["test-case-name"] = "test-min-replicas-zero"
		m.Spec.MinReplicas = 0

		// In external mode, minReplicas=0 is allowed to be created (we just log a warning)
		err := testK8sClient.Create(testCtx, m)
		require.NoError(t, err)

		// Clean up
		_ = testK8sClient.Delete(testCtx, m)
	})
}
