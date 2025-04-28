package integration

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "github.com/substratusai/kubeai/api/k8s/v1"
	corev1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestModelProfiles tests that profiles are applied as expected.
func TestJSONPatch(t *testing.T) {
	sysCfg := baseSysCfg(t)
	initTest(t, sysCfg)
	priorityClass := &schedulingv1.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-priority-class",
		},
	}
	require.NoError(t, testK8sClient.Create(testCtx, priorityClass))

	// Construct a Model object with MinReplicas set to 0.
	m := modelForTest(t)
	// Make sure there is a Pod created to run assertions against.
	m.Spec.MinReplicas = 1
	const expectedPriorityClassName = "test-priority-class"
	m.Spec.JSONPatches = []v1.JSONPatch{
		{
			Op:    "add",
			Path:  "/spec/priorityClassName",
			Value: expectedPriorityClassName,
		},
	}
	// Create the Model object in the Kubernetes cluster.
	require.NoError(t, testK8sClient.Create(testCtx, m))

	var pod *corev1.Pod
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		podList := &corev1.PodList{}
		if !assert.NoError(t, testK8sClient.List(testCtx, podList, client.InNamespace(testNS), client.MatchingLabels{"model": m.Name})) {
			return
		}
		if !assert.Len(t, podList.Items, 1) {
			return
		}
		pod = &podList.Items[0]

		// Verify the priorityClassName is set on the pod
		assert.Equal(t, expectedPriorityClassName, pod.Spec.PriorityClassName)

	}, 5*time.Second, time.Second/10, "Resource profile and ModelServerPods settings should be applied to the model Pod object")

	const userImage = "my-repo.com/my-repo/my-image:latest"
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		if !assert.NoError(t, testK8sClient.Get(testCtx, client.ObjectKeyFromObject(m), m)) {
			return
		}
		m.Spec.Image = userImage
		require.NoError(t, testK8sClient.Update(testCtx, m))
	}, 2*time.Second, time.Second/10, "Update model with user specified image")

	require.NoError(t, testK8sClient.Delete(testCtx, pod))
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		podList := &corev1.PodList{}
		if !assert.NoError(t, testK8sClient.List(testCtx, podList, client.InNamespace(testNS), client.MatchingLabels{"model": m.Name})) {
			return
		}
		if !assert.Len(t, podList.Items, 1) {
			return
		}
		pod = &podList.Items[0]

		// The Pod should have a single container named "server".
		container := mustFindPodContainerByName(t, pod, "server")
		assert.Equal(t, userImage, container.Image)
	}, 15*time.Second, time.Second/10, "User specified image should be respected")
}
