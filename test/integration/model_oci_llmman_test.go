package integration

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestModelOCIViaLlmman tests that oci://...?via=llmman pulls through an init
// container into a volume the server container reads.
func TestModelOCIViaLlmman(t *testing.T) {
	sysCfg := baseSysCfg(t)
	sysCfg.ModelLoading.Llmman = "llmman-loader"
	sysCfg.ModelLoading.LlmmanStore = "llmman-store"
	initTest(t, sysCfg)

	m := modelForTest(t)
	m.Spec.URL = "oci://registry.test/org/model:tag?via=llmman"
	m.Spec.MinReplicas = 1
	require.NoError(t, testK8sClient.Create(testCtx, m))

	require.EventuallyWithT(t, func(t *assert.CollectT) {
		podList := &corev1.PodList{}
		if !assert.NoError(t, testK8sClient.List(testCtx, podList, client.InNamespace(testNS), client.MatchingLabels{"model": m.Name})) {
			return
		}
		if !assert.Len(t, podList.Items, 1) {
			return
		}
		pod := &podList.Items[0]

		if assert.Len(t, pod.Spec.InitContainers, 1) {
			puller := pod.Spec.InitContainers[0]
			assert.Equal(t, "llmman-loader", puller.Image)
			assert.Equal(t, []string{"registry.test/org/model:tag", "/model"}, puller.Args)
		}
		server := mustFindPodContainerByName(t, pod, "server")
		assert.Contains(t, server.VolumeMounts, corev1.VolumeMount{Name: "model", MountPath: "/model", ReadOnly: true})
		assert.Contains(t, server.Args, "--model=/model")
	}, 10*time.Second, time.Second/10, "Pod should pull the model in an init container")
}
