package integration

import (
	"testing"
	"time"

	"github.com/kubeai-project/kubeai/internal/k8sutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestModelPodUpdateRollout tests that an update to a Model object triggers the recreation
// of the corresponding Pods.
func TestModelPodUpdateRollout(t *testing.T) {
	initTest(t, baseSysCfg(t))

	// Create a Model object.
	m := modelForTest(t)
	m.Spec.MinReplicas = 3
	m.Spec.MaxReplicas = ptr.To[int32](3)

	require.NoError(t, testK8sClient.Create(testCtx, m))

	// Expect 3 Pods to be created.
	// NOTE: Wait time needs to be long enough to take into account
	// the rate limiting the Model Reconciler has for Pod updates.
	requireModelPods(t, m, 3, "3 Pods should be created", 5*time.Second)

	// Update the Model object.
	const newArg = "--my-new-arg-added-in-testcase"
	updateModel(t, m, func() { m.Spec.Args = []string{newArg} }, "Adding a new arg to the Model")

	// Expect 3 Pods to be created with the new arg.
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		podList := &corev1.PodList{}
		if !assert.NoError(t, testK8sClient.List(testCtx, podList, client.InNamespace(testNS), client.MatchingLabels{"model": m.Name})) {
			return
		}

		for _, pod := range podList.Items {
			if !k8sutils.PodIsReady(&pod) {
				// Update Pod to be Ready so Rollout can proceed.
				pod.Status.Conditions = append(pod.Status.Conditions, corev1.PodCondition{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				})
				assert.NoError(t, testK8sClient.Status().Update(testCtx, &pod))
				return
			}
			if !assert.Containsf(t, pod.Spec.Containers[0].Args, newArg, "Pod %q should have the new arg", pod.Name) {
				return
			}
		}

		assert.Equal(t, 3, len(podList.Items), "Exactly 3 Pods should exist")

		// NOTE: Wait time needs to be long enough to take into account
		// the rate limiting the Model Reconciler has for Pod updates.
	}, 15*time.Second, 1*time.Second, "Exactly 3 Pods should exist (with the new arg)")
}
