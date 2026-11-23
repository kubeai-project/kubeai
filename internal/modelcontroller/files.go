package modelcontroller

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	kubeaiv1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// getModelFilesConfigMapName returns the name of the ConfigMap for a model
func getModelFilesConfigMapName(model *kubeaiv1.Model) string {
	return fmt.Sprintf("model-%s-files", model.Name)
}

// ensureModelFilesConfigMap ensures that the ConfigMap for model files exists and is up to date
func (r *ModelReconciler) ensureModelFilesConfigMap(ctx context.Context, model *kubeaiv1.Model) error {
	log := log.FromContext(ctx)
	configMapName := getModelFilesConfigMapName(model)

	// Build the expected data map with sanitized keys
	expectedData := make(map[string]string)
	for _, file := range model.Spec.Files {
		sanitizedKey := fileConfigMapKey(file.Path)
		expectedData[sanitizedKey] = file.Content
	}

	// Check if ConfigMap exists
	existingConfigMap := &corev1.ConfigMap{}
	err := r.Get(ctx, client.ObjectKey{Namespace: model.Namespace, Name: configMapName}, existingConfigMap)

	if err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("getting model files configmap: %w", err)
		}

		// ConfigMap doesn't exist, create it
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: model.Namespace,
			},
			Data: expectedData,
		}

		// Set owner reference
		if err := ctrl.SetControllerReference(model, configMap, r.Scheme); err != nil {
			return fmt.Errorf("setting controller reference on model files configmap: %w", err)
		}

		if err := r.Create(ctx, configMap); err != nil {
			return fmt.Errorf("creating model files configmap: %w", err)
		}

		log.Info("Created model files ConfigMap", "configMapName", configMapName)
		return nil
	} else if len(model.Spec.Files) == 0 {
		if err := r.Delete(ctx, existingConfigMap); err != nil {
			return fmt.Errorf("deleting empty model files configmap: %w", err)
		}
		return nil
	}

	// ConfigMap exists, check if update is needed
	if !reflect.DeepEqual(existingConfigMap.Data, expectedData) {
		existingConfigMap.Data = expectedData
		if err := r.Update(ctx, existingConfigMap); err != nil {
			return fmt.Errorf("updating model files configmap: %w", err)
		}
		log.Info("Updated model files ConfigMap", "configMapName", configMapName)
	}

	return nil
}

// patchFileVolumes adds volume and volume mount for the model files if specified.
func patchFileVolumes(spec *corev1.PodSpec, model *kubeaiv1.Model) {
	if len(model.Spec.Files) == 0 {
		return
	}

	// Add volume for the ConfigMap
	spec.Volumes = append(spec.Volumes, corev1.Volume{
		Name: modelFilesVolumeName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: getModelFilesConfigMapName(model),
				},
			},
		},
	})
	for i := range spec.Containers {
		if spec.Containers[i].Name == serverContainerName {
			spec.Containers[i].VolumeMounts = append(spec.Containers[i].VolumeMounts, modelFileVolumeMounts(model)...)
		}
	}
}

// modelFileVolumeMounts returns a read-only mount of the model files volume for
// each file in the model spec. patchFileVolumes adds the volume itself.
func modelFileVolumeMounts(model *kubeaiv1.Model) []corev1.VolumeMount {
	mounts := make([]corev1.VolumeMount, 0, len(model.Spec.Files))
	for _, file := range model.Spec.Files {
		mounts = append(mounts, corev1.VolumeMount{
			Name:      modelFilesVolumeName,
			MountPath: file.Path,
			SubPath:   fileConfigMapKey(file.Path),
			ReadOnly:  true,
		})
	}
	return mounts
}

// fileConfigMapKey replaces illegal "/" characters in ConfigMap keys.
// ConfigMap keys must consist of alphanumeric characters, '-', '_' or '.'
func fileConfigMapKey(filePath string) string {
	return strings.ReplaceAll(filePath, "/", "_")
}
