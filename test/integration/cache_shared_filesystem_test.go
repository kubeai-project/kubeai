package integration

import (
	"fmt"
	"testing"
	"time"

	v1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/kubeai-project/kubeai/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestCacheSharedFilesystem(t *testing.T) {
	// Configure cache profile
	const (
		cacheProfileName = "my-test-cache"
	)
	sysCfg := baseSysCfg(t)
	sysCfg.CacheProfiles = map[string]config.CacheProfile{
		cacheProfileName: {
			SharedFilesystem: &config.CacheSharedFilesystem{
				StorageClassName:     "my-storage-class",
				PersistentVolumeName: "my-pv",
			},
		},
	}
	initTest(t, sysCfg)

	// Create a Model with cache profile
	m := modelForTest(t)
	m.Spec.MinReplicas = 1
	m.Spec.CacheProfile = cacheProfileName
	require.NoError(t, testK8sClient.Create(testCtx, m))

	// Assert that the expected PVC is created
	pvc := &corev1.PersistentVolumeClaim{}
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		assert.NoError(t, testK8sClient.Get(testCtx, types.NamespacedName{
			Namespace: m.Namespace,
			Name:      fmt.Sprintf("shared-model-cache-%s", cacheProfileName),
		}, pvc))
	}, 15*time.Second, time.Second/10, "PVC should be created")
	require.Equal(t, ptr.To("my-storage-class"), pvc.Spec.StorageClassName)
	require.Equal(t, "my-pv", pvc.Spec.VolumeName)

	// Assert that the model loader Job is created
	loaderJob := &batchv1.Job{}
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		assert.NoError(t, testK8sClient.Get(testCtx, types.NamespacedName{
			Namespace: m.Namespace,
			Name:      fmt.Sprintf("load-cache-%s", m.Name),
		}, loaderJob))
	}, 5*time.Second, time.Second/10, "Loader Job should be created")

	// Update the Job to have a completed status
	requireUpdateJobAsCompleted(t, loaderJob)

	// Assert that the PVC was updated with an annotation for the downloaded model
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		// get name from object
		if !assert.NoError(t, testK8sClient.Get(testCtx, client.ObjectKeyFromObject(pvc), pvc)) {
			return
		}
		require.Contains(t, pvc.Annotations, "models.kubeai.org/"+m.Name)
	}, 5*time.Second, time.Second/10, "PVC should be updated with model annotation")

	// Assert that the loader job is deleted
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		j := &batchv1.Job{}
		err := testK8sClient.Get(testCtx, client.ObjectKeyFromObject(loaderJob), j)
		if err != nil {
			assert.True(t, apierrors.IsNotFound(err))
		} else {
			// Account for finalizers like foreground deletion.
			assert.NotNil(t, j.DeletionTimestamp)
		}
	}, 5*time.Second, time.Second/10, "Loader Job should be deleted")

	// Assert that the Model status is updated and a finalizer is added.
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		if !assert.NoError(t, testK8sClient.Get(testCtx, client.ObjectKeyFromObject(m), m)) {
			return
		}
		if !assert.NotNil(t, m.Status.Cache) {
			return
		}
		assert.True(t, m.Status.Cache.Loaded)
		assert.Contains(t, m.Finalizers, v1.ModelCacheEvictionFinalizer)
	}, 10*time.Second, time.Second/10, "Model status & finalizers should be updated")

	// Assert that the model engine Pod is created
	podList := &corev1.PodList{}
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		if !assert.NoError(t, testK8sClient.List(testCtx, podList, client.InNamespace(testNS), client.MatchingLabels{"model": m.Name})) {
			return
		}
		if !assert.Len(t, podList.Items, 1) {
			return
		}
	}, 15*time.Second, time.Second/10, "Model Pods should be created")

	// Assert that the model engine Pod is configured to use the shared PVC
	var volFound bool
	for _, v := range podList.Items[0].Spec.Volumes {
		if v.PersistentVolumeClaim != nil && v.PersistentVolumeClaim.ClaimName == pvc.Name {
			volFound = true
			break
		}
	}
	require.True(t, volFound, "Model Pod should have a volume mounted from the shared PVC")

	// Delete the Model
	require.NoError(t, testK8sClient.Delete(testCtx, m), "Model deletion should succeed")

	// Assert that the cache eviction Job is created
	evictJob := &batchv1.Job{}
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		assert.NoError(t, testK8sClient.Get(testCtx, types.NamespacedName{
			Namespace: m.Namespace,
			Name:      fmt.Sprintf("evict-cache-%s", m.Name),
		}, evictJob))
	}, 5*time.Second, time.Second/10, "Eviction Job should be created")

	// Update the Job to have a completed status
	requireUpdateJobAsCompleted(t, evictJob)

	// Assert that the Model is gone (finalized)
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := testK8sClient.Get(testCtx, client.ObjectKeyFromObject(m), m)
		if !assert.Error(t, err) {
			return
		}
		assert.True(t, apierrors.IsNotFound(err))
	}, 5*time.Second, time.Second/10, "Model should be finalized")

	// Assert that eviction Job is deleted.
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		j := &batchv1.Job{}
		err := testK8sClient.Get(testCtx, client.ObjectKeyFromObject(evictJob), j)
		if err != nil {
			assert.True(t, apierrors.IsNotFound(err))
		} else {
			// Account for finalizers like foreground deletion.
			assert.NotNil(t, j.DeletionTimestamp)
		}
	}, 5*time.Second, time.Second/10, "Eviction Job should be deleted")

	// Assert that all Job Pods are deleted.
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		jobPodList := &corev1.PodList{}
		for _, jobName := range []string{
			loaderJob.Name,
			evictJob.Name,
		} {
			if !assert.NoError(t, testK8sClient.List(testCtx, jobPodList, client.InNamespace(testNS), client.MatchingLabels{
				batchv1.JobNameLabel: jobName,
			})) {
				return
			}
			assert.Len(t, jobPodList.Items, 0)
		}
	}, 5*time.Second, time.Second/10, "All Job Pods should be deleted")
}
