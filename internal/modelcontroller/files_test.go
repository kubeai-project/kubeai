package modelcontroller

import (
	"testing"

	kubeaiv1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_fileConfigMapKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple path",
			input:    "/tmp/file.txt",
			expected: "_tmp_file.txt",
		},
		{
			name:     "complex path",
			input:    "/usr/local/configs/chat-template.jinja",
			expected: "_usr_local_configs_chat-template.jinja",
		},
		{
			name:     "no slashes",
			input:    "filename.txt",
			expected: "filename.txt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := fileConfigMapKey(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func Test_patchFileVolumes(t *testing.T) {
	tests := []struct {
		name         string
		podSpec      *corev1.PodSpec
		model        *kubeaiv1.Model
		expectedSpec *corev1.PodSpec
	}{
		{
			name: "no files",
			podSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: serverContainerName,
					},
				},
			},
			model: &kubeaiv1.Model{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-model",
				},
				Spec: kubeaiv1.ModelSpec{
					Files: []kubeaiv1.File{},
				},
			},
			expectedSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: serverContainerName,
					},
				},
			},
		},
		{
			name: "single file",
			podSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: serverContainerName,
					},
				},
			},
			model: &kubeaiv1.Model{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-model",
				},
				Spec: kubeaiv1.ModelSpec{
					Files: []kubeaiv1.File{
						{
							Path:    "/config/prompt.txt",
							Content: "This is a test prompt",
						},
					},
				},
			},
			expectedSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: serverContainerName,
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      modelFilesVolumeName,
								MountPath: "/config/prompt.txt",
								SubPath:   "_config_prompt.txt",
								ReadOnly:  true,
							},
						},
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: modelFilesVolumeName,
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "model-test-model-files",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "multiple files",
			podSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: serverContainerName,
					},
				},
			},
			model: &kubeaiv1.Model{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-model",
				},
				Spec: kubeaiv1.ModelSpec{
					Files: []kubeaiv1.File{
						{
							Path:    "/config/prompt.txt",
							Content: "This is a test prompt",
						},
						{
							Path:    "/templates/chat.jinja",
							Content: "{{ system }} {{ user }} {{ assistant }}",
						},
					},
				},
			},
			expectedSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: serverContainerName,
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      modelFilesVolumeName,
								MountPath: "/config/prompt.txt",
								SubPath:   "_config_prompt.txt",
								ReadOnly:  true,
							},
							{
								Name:      modelFilesVolumeName,
								MountPath: "/templates/chat.jinja",
								SubPath:   "_templates_chat.jinja",
								ReadOnly:  true,
							},
						},
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: modelFilesVolumeName,
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "model-test-model-files",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "multiple containers but only patch server container",
			podSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "init-container",
					},
					{
						Name: serverContainerName,
					},
					{
						Name: "sidecar",
					},
				},
			},
			model: &kubeaiv1.Model{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-model",
				},
				Spec: kubeaiv1.ModelSpec{
					Files: []kubeaiv1.File{
						{
							Path:    "/config/prompt.txt",
							Content: "This is a test prompt",
						},
					},
				},
			},
			expectedSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "init-container",
					},
					{
						Name: serverContainerName,
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      modelFilesVolumeName,
								MountPath: "/config/prompt.txt",
								SubPath:   "_config_prompt.txt",
								ReadOnly:  true,
							},
						},
					},
					{
						Name: "sidecar",
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: modelFilesVolumeName,
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "model-test-model-files",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "server container not found",
			podSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "some-other-container",
					},
				},
			},
			model: &kubeaiv1.Model{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-model",
				},
				Spec: kubeaiv1.ModelSpec{
					Files: []kubeaiv1.File{
						{
							Path:    "/config/prompt.txt",
							Content: "This is a test prompt",
						},
					},
				},
			},
			expectedSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "some-other-container",
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: modelFilesVolumeName,
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "model-test-model-files",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "existing volumes and mounts",
			podSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: serverContainerName,
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "existing-volume",
								MountPath: "/existing/path",
							},
						},
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: "existing-volume",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					},
				},
			},
			model: &kubeaiv1.Model{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-model",
				},
				Spec: kubeaiv1.ModelSpec{
					Files: []kubeaiv1.File{
						{
							Path:    "/config/prompt.txt",
							Content: "This is a test prompt",
						},
					},
				},
			},
			expectedSpec: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: serverContainerName,
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "existing-volume",
								MountPath: "/existing/path",
							},
							{
								Name:      modelFilesVolumeName,
								MountPath: "/config/prompt.txt",
								SubPath:   "_config_prompt.txt",
								ReadOnly:  true,
							},
						},
					},
				},
				Volumes: []corev1.Volume{
					{
						Name: "existing-volume",
						VolumeSource: corev1.VolumeSource{
							EmptyDir: &corev1.EmptyDirVolumeSource{},
						},
					},
					{
						Name: modelFilesVolumeName,
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "model-test-model-files",
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patchFileVolumes(tt.podSpec, tt.model)
			assert.Equal(t, tt.expectedSpec, tt.podSpec)
		})
	}
}
