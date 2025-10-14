package integration

import (
	"testing"
	"time"

	kubeaiv1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestModelFiles tests that model files are properly mounted in pods.
func TestModelFiles(t *testing.T) {
	sysCfg := baseSysCfg(t)
	initTest(t, sysCfg)

	// Construct a Model object
	m := modelForTest(t)
	m.Spec.MinReplicas = 1

	// Add files to the model
	m.Spec.Files = []kubeaiv1.File{
		{
			Path:    "/config/prompt-template.txt",
			Content: "This is a prompt template for testing",
		},
		{
			Path:    "/templates/chat-template.jinja",
			Content: "{{ system }} {{ user }} {{ assistant }}",
		},
	}

	// Create the Model object in the Kubernetes cluster
	require.NoError(t, testK8sClient.Create(testCtx, m))

	// Verify the ConfigMap is created with correct content
	configMapName := "model-" + m.Name + "-files"
	configMap := &corev1.ConfigMap{}

	require.EventuallyWithT(t, func(t *assert.CollectT) {
		if !assert.NoError(t, testK8sClient.Get(testCtx, client.ObjectKey{Namespace: testNS, Name: configMapName}, configMap)) {
			return
		}

		// Check ConfigMap has the expected data with sanitized keys
		assert.Equal(t, 2, len(configMap.Data))
		assert.Equal(t, "This is a prompt template for testing", configMap.Data["_config_prompt-template.txt"])
		assert.Equal(t, "{{ system }} {{ user }} {{ assistant }}", configMap.Data["_templates_chat-template.jinja"])

		// Check that the ConfigMap is owned by the model
		assert.Equal(t, 1, len(configMap.OwnerReferences))
		assert.Equal(t, "Model", configMap.OwnerReferences[0].Kind)
		assert.Equal(t, m.Name, configMap.OwnerReferences[0].Name)
	}, 5*time.Second, time.Second/10, "ConfigMap should be created with correct content")

	// Verify Pod has correct volume and volume mounts
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

		// Verify the volume exists
		volumeFound := false
		for _, volume := range pod.Spec.Volumes {
			if volume.Name == "model-files" {
				volumeFound = true
				assert.NotNil(t, volume.ConfigMap)
				assert.Equal(t, configMapName, volume.ConfigMap.Name)
			}
		}
		assert.True(t, volumeFound, "Pod should have model-files volume")

		// The Pod should have a single container named "server" with volume mounts
		container := mustFindPodContainerByName(t, pod, "server")

		promptMountFound := false
		templateMountFound := false
		for _, mount := range container.VolumeMounts {
			if mount.Name == "model-files" {
				if mount.MountPath == "/config/prompt-template.txt" {
					promptMountFound = true
					assert.Equal(t, "_config_prompt-template.txt", mount.SubPath)
					assert.True(t, mount.ReadOnly)
				}
				if mount.MountPath == "/templates/chat-template.jinja" {
					templateMountFound = true
					assert.Equal(t, "_templates_chat-template.jinja", mount.SubPath)
					assert.True(t, mount.ReadOnly)
				}
			}
		}

		assert.True(t, promptMountFound, "Server container should mount prompt file")
		assert.True(t, templateMountFound, "Server container should mount template file")
	}, 5*time.Second, time.Second/10, "Pod should be created with correct volume mounts")

	// Test updating files
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		if !assert.NoError(t, testK8sClient.Get(testCtx, client.ObjectKeyFromObject(m), m)) {
			return
		}

		// Add a new file
		m.Spec.Files = append(m.Spec.Files, kubeaiv1.File{
			Path:    "/config/new-file.txt",
			Content: "This is a new file",
		})

		// Modify an existing file
		m.Spec.Files[0].Content = "Updated prompt template content"

		assert.NoError(t, testK8sClient.Update(testCtx, m))
	}, 2*time.Second, time.Second/10, "Update model with new and modified files")

	// Verify ConfigMap is updated
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		if !assert.NoError(t, testK8sClient.Get(testCtx, client.ObjectKey{Namespace: testNS, Name: configMapName}, configMap)) {
			return
		}

		assert.Equal(t, 3, len(configMap.Data), "ConfigMap should have 3 data entries after update")
		assert.Equal(t, "Updated prompt template content", configMap.Data["_config_prompt-template.txt"], "ConfigMap should have updated content")
		assert.Equal(t, "This is a new file", configMap.Data["_config_new-file.txt"], "ConfigMap should have new file content")
	}, 5*time.Second, time.Second/10, "ConfigMap should be updated with new and modified files")

	// Verify the new Pod has the updated volume mounts
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		podList := &corev1.PodList{}
		if !assert.NoError(t, testK8sClient.List(testCtx, podList, client.InNamespace(testNS), client.MatchingLabels{"model": m.Name})) {
			return
		}
		if !assert.Len(t, podList.Items, 1) {
			return
		}
		pod = &podList.Items[0]

		// The Pod should have a single container named "server" with updated volume mounts
		container := mustFindPodContainerByName(t, pod, "server")

		newFileMountFound := false
		for _, mount := range container.VolumeMounts {
			if mount.Name == "model-files" && mount.MountPath == "/config/new-file.txt" {
				newFileMountFound = true
				assert.Equal(t, "_config_new-file.txt", mount.SubPath)
				assert.True(t, mount.ReadOnly)
			}
		}

		assert.True(t, newFileMountFound, "Server container should mount new file")
	}, 15*time.Second, time.Second/10, "New Pod should be created with updated volume mounts")

	// Test updating files to empty
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		if !assert.NoError(t, testK8sClient.Get(testCtx, client.ObjectKeyFromObject(m), m)) {
			return
		}

		// Set files to empty
		m.Spec.Files = []kubeaiv1.File{}

		assert.NoError(t, testK8sClient.Update(testCtx, m))
	}, 2*time.Second, time.Second/10, "Update model with empty files")

	// Verify ConfigMap is deleted
	require.EventuallyWithT(t, func(t *assert.CollectT) {
		err := testK8sClient.Get(testCtx, client.ObjectKey{Namespace: testNS, Name: configMapName}, configMap)
		assert.True(t, apierrors.IsNotFound(err), "ConfigMap should be deleted when files is empty")
	}, 5*time.Second, time.Second/10, "ConfigMap should be deleted when files is empty")
}
