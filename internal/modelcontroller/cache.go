package modelcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	kubeaiv1 "github.com/substratusai/kubeai/api/k8s/v1"
	"github.com/substratusai/kubeai/internal/k8sutils"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type PVCModelAnnotationValue struct {
	UID       string    `json:"uid"`
	Timestamp time.Time `json:"timestamp"`
}

func (r *ModelReconciler) reconcileCache(ctx context.Context, model *kubeaiv1.Model, cfg ModelConfig) (ctrl.Result, error) {
	if model.Status.Cache == nil {
		model.Status.Cache = &kubeaiv1.ModelStatusCache{}
	}

	modelDeleted := model.DeletionTimestamp != nil

	pvc := &corev1.PersistentVolumeClaim{}
	var pvcExists bool
	if err := r.Client.Get(ctx, types.NamespacedName{
		Namespace: model.Namespace,
		Name:      cachePVCName(model, cfg),
	}, pvc); err != nil {
		if apierrors.IsNotFound(err) {
			pvcExists = false
		} else {
			return ctrl.Result{}, fmt.Errorf("getting cache PVC: %w", err)
		}
	} else {
		pvcExists = true
	}

	// Create PVC if not exists.
	if !pvcExists {
		if !modelDeleted {
			pvc = r.cachePVCForModel(model, cfg)
			// TODO: Set controller reference on PVC for 1:1 Model to PVC situations
			// such as Google Hyperdisk ML.
			//if err := controllerutil.SetControllerReference(model, pvc, r.Scheme); err != nil {
			//	return ctrl.Result{}, fmt.Errorf("setting controller reference on pvc: %w", err)
			//}
			if err := r.Create(ctx, pvc); err != nil {
				return ctrl.Result{}, fmt.Errorf("creating cache PVC: %w", err)
			}
		}
	}

	// Caches that are shared across multiple Models require model-specific cleanup.
	if cfg.CacheProfile.SharedFilesystem != nil {
		if controllerutil.AddFinalizer(model, kubeaiv1.ModelCacheEvictionFinalizer) {
			if err := r.Update(ctx, model); err != nil {
				return ctrl.Result{}, fmt.Errorf("adding cache deletion finalizer: %w", err)
			}
		}

	}
	// NOTE: .Spec.CacheProfile and .Spec.URL are immutable, so we don't need to check if they
	// have changed in order to evict a stale cache.

	loadJob := &batchv1.Job{}
	var jobExists bool
	if err := r.Client.Get(ctx, types.NamespacedName{
		Namespace: model.Namespace,
		Name:      loadCacheJobName(model),
	}, loadJob); err != nil {
		if apierrors.IsNotFound(err) {
			jobExists = false
		} else {
			return ctrl.Result{}, fmt.Errorf("getting cache job: %w", err)
		}
	} else {
		jobExists = true
	}

	pvcModelAnn, err := parsePVCModelAnnotation(pvc, model.Name)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("parsing pvc model annotation: %w", err)
	}

	// Run Job to populate PVC if not already downloaded.
	if pvcModelAnn.UID != string(model.UID) {
		// Ensure the download job exists.
		if !jobExists {
			loadJob = r.loadCacheJobForModel(model, cfg)
			if err := ctrl.SetControllerReference(model, loadJob, r.Scheme); err != nil {
				return ctrl.Result{}, fmt.Errorf("setting controller reference on job: %w", err)
			}
			if err := r.Create(ctx, loadJob); err != nil {
				return ctrl.Result{}, fmt.Errorf("creating job: %w", err)
			}
			return ctrl.Result{}, errReturnEarly
		}

		if !k8sutils.IsJobCompleted(loadJob) {
			return ctrl.Result{}, errReturnEarly
		}
		if err := r.updatePVCModelAnnotation(ctx, pvc, model.Name, PVCModelAnnotationValue{
			UID:       string(model.UID),
			Timestamp: time.Now(),
		}); err != nil {
			return ctrl.Result{}, fmt.Errorf("setting pvc model annotation: %w", err)
		}
	}
	model.Status.Cache.Loaded = pvcModelAnn.UID == string(model.UID)

	if jobExists {
		// Cache loading completed, delete Job to avoid accumulating a mess of completed Jobs.
		// Use foreground deletion policy to ensure the Pods are deleted as well.
		if err := r.Delete(ctx, loadJob, client.PropagationPolicy(metav1.DeletePropagationForeground)); err != nil {
			return ctrl.Result{}, fmt.Errorf("deleting job: %w", err)
		}
	}

	return ctrl.Result{}, nil
}

func (r *ModelReconciler) finalizeCache(ctx context.Context, model *kubeaiv1.Model, cfg ModelConfig) error {
	pvc := &corev1.PersistentVolumeClaim{}
	var pvcExists bool
	if err := r.Client.Get(ctx, types.NamespacedName{
		Namespace: model.Namespace,
		Name:      cachePVCName(model, cfg),
	}, pvc); err != nil {
		if !apierrors.IsNotFound(err) {
			return fmt.Errorf("getting cache PVC: %w", err)
		}
	} else {
		pvcExists = true
	}

	if !pvcExists || pvc.DeletionTimestamp != nil {
		// If the PVC is not found or is already being deleted, delete all cache jobs and pods.
		// No need trying to update the PVC annotations or perform other cleanup.
		if err := r.deleteAllCacheJobsAndPods(ctx, model); err != nil {
			return fmt.Errorf("deleting all cache jobs and pods: %w", err)
		}
		if controllerutil.RemoveFinalizer(model, kubeaiv1.ModelCacheEvictionFinalizer) {
			if err := r.Update(ctx, model); err != nil {
				return fmt.Errorf("removing cache deletion finalizer: %w", err)
			}
		}
		return nil
	}

	if controllerutil.ContainsFinalizer(model, kubeaiv1.ModelCacheEvictionFinalizer) {
		evictJob := &batchv1.Job{}
		var jobExists bool
		if err := r.Client.Get(ctx, types.NamespacedName{
			Namespace: model.Namespace,
			Name:      evictCacheJobName(model),
		}, evictJob); err != nil {
			if apierrors.IsNotFound(err) {
				jobExists = false
			} else {
				return fmt.Errorf("getting cache deletion job: %w", err)
			}
		} else {
			jobExists = true
		}

		if !jobExists {
			job := r.evictCacheJobForModel(model, cfg)
			if err := ctrl.SetControllerReference(model, job, r.Scheme); err != nil {
				return fmt.Errorf("setting controller reference on cache deletion job: %w", err)
			}
			if err := r.Create(ctx, job); err != nil {
				return fmt.Errorf("creating cache deletion job: %w", err)
			}
			return errReturnEarly
		} else {
			// Wait for the Job to complete.
			if !k8sutils.IsJobCompleted(evictJob) {
				return errReturnEarly
			}

			// Delete the Model from the PVC annotation.
			if pvc.Annotations != nil {
				if _, ok := pvc.Annotations[kubeaiv1.PVCModelAnnotation(model.Name)]; ok {
					delete(pvc.Annotations, kubeaiv1.PVCModelAnnotation(model.Name))
					if err := r.Update(ctx, pvc); err != nil {
						return fmt.Errorf("updating PVC, removing cache annotation: %w", err)
					}
				}
			}
		}

		controllerutil.RemoveFinalizer(model, kubeaiv1.ModelCacheEvictionFinalizer)
		if err := r.Update(ctx, model); err != nil {
			return fmt.Errorf("removing cache deletion finalizer: %w", err)
		}
	}

	if err := r.deleteAllCacheJobsAndPods(ctx, model); err != nil {
		return fmt.Errorf("deleting all cache jobs and pods: %w", err)
	}

	return nil
}

func (r *ModelReconciler) deleteAllCacheJobsAndPods(ctx context.Context, model *kubeaiv1.Model) error {
	jobNames := []string{
		loadCacheJobName(model),
		evictCacheJobName(model),
	}

	for _, jobName := range jobNames {
		if err := r.Delete(ctx, &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: model.Namespace,
				Name:      jobName,
			},
		}); err != nil {
			if !apierrors.IsNotFound(err) {
				return fmt.Errorf("deleting job %q: %w", jobName, err)
			}
		}

		// NOTE: There are different conditions in which Pods might not be deleted by the Job controller
		// after a Job is deleted.
		if err := r.DeleteAllOf(ctx, &corev1.Pod{}, client.InNamespace(model.Namespace), client.MatchingLabels{
			batchv1.JobNameLabel: jobName,
		}); err != nil {
			if !apierrors.IsNotFound(err) {
				return fmt.Errorf("deleting pods for job %q: %w", jobName, err)
			}
		}
	}

	return nil
}

func parsePVCModelAnnotation(pvc *corev1.PersistentVolumeClaim, modelName string) (PVCModelAnnotationValue, error) {
	pvcModelStatusJSON := k8sutils.GetAnnotation(pvc, kubeaiv1.PVCModelAnnotation(modelName))
	if pvcModelStatusJSON == "" {
		return PVCModelAnnotationValue{}, nil
	}
	var status PVCModelAnnotationValue
	if err := json.Unmarshal([]byte(pvcModelStatusJSON), &status); err != nil {
		return PVCModelAnnotationValue{}, fmt.Errorf("unmarshalling pvc model status: %w", err)
	}
	return status, nil
}

func (r *ModelReconciler) updatePVCModelAnnotation(ctx context.Context, pvc *corev1.PersistentVolumeClaim, modelName string, status PVCModelAnnotationValue) error {
	statusJSON, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("marshalling pvc model status: %w", err)
	}
	k8sutils.SetAnnotation(pvc, kubeaiv1.PVCModelAnnotation(modelName), string(statusJSON))
	if err := r.Client.Update(ctx, pvc); err != nil {
		return fmt.Errorf("updating pvc: %w", err)
	}
	return nil
}

func (r *ModelReconciler) cachePVCForModel(m *kubeaiv1.Model, c ModelConfig) *corev1.PersistentVolumeClaim {
	pvc := corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cachePVCName(m, c),
			Namespace: m.Namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{},
	}
	switch {
	case c.CacheProfile.SharedFilesystem != nil:
		pvc.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany}
		storageClassName := c.CacheProfile.SharedFilesystem.StorageClassName
		pvc.Spec.StorageClassName = &storageClassName
		pvc.Spec.VolumeName = c.CacheProfile.SharedFilesystem.PersistentVolumeName
		pvc.Spec.Resources.Requests = corev1.ResourceList{
			// https://discuss.huggingface.co/t/how-to-get-model-size/11038/7
			corev1.ResourceStorage: resource.MustParse("10Gi"),
		}
	default:
		panic("unsupported cache profile, this point should not be reached")
	}
	return &pvc
}

func cachePVCName(m *kubeaiv1.Model, c ModelConfig) string {
	switch {
	case c.CacheProfile.SharedFilesystem != nil:
		// One PVC for all models.
		return fmt.Sprintf("shared-model-cache-%s", m.Spec.CacheProfile)
	default:
		// One PVC per model.
		return fmt.Sprintf("model-cache-%s-%s", m.Name, m.UID[0:7])
	}
}

func (r *ModelReconciler) loadCacheJobForModel(m *kubeaiv1.Model, c ModelConfig) *batchv1.Job {
	var env []corev1.EnvVar
	var envKeys []string
	for key := range m.Spec.Env {
		envKeys = append(envKeys, key)
	}
	sort.Strings(envKeys)
	for _, key := range envKeys {
		env = append(env, corev1.EnvVar{
			Name:  key,
			Value: m.Spec.Env[key],
		})
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      loadCacheJobName(m),
			Namespace: m.Namespace,
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: ptr.To[int32](60),
			Parallelism:             ptr.To[int32](1),
			Completions:             ptr.To[int32](1),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:   corev1.RestartPolicyOnFailure,
					SecurityContext: r.ModelServerPods.ModelPodSecurityContext,
					Containers: []corev1.Container{
						{
							Name:            "loader",
							Env:             env,
							SecurityContext: r.ModelServerPods.ModelContainerSecurityContext,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "model",
									MountPath: modelCacheDir(m),
									SubPath:   strings.TrimPrefix(modelCacheDir(m), "/"),
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "model",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: cachePVCName(m, c),
								},
							},
						},
					},
				},
			},
		},
	}

	job.Spec.Template.Spec.Containers[0].Image = r.ModelLoaders.Image
	job.Spec.Template.Spec.Containers[0].Args = []string{
		m.Spec.URL,
		modelCacheDir(m),
	}
	c.Source.modelSourcePodAdditions.applyToPodSpec(&job.Spec.Template.Spec, 0)

	return job
}

func (r *ModelReconciler) evictCacheJobForModel(m *kubeaiv1.Model, c ModelConfig) *batchv1.Job {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      evictCacheJobName(m),
			Namespace: m.Namespace,
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: ptr.To[int32](60),
			Parallelism:             ptr.To[int32](1),
			Completions:             ptr.To[int32](1),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:   corev1.RestartPolicyOnFailure,
					SecurityContext: r.ModelServerPods.ModelPodSecurityContext,
					Containers: []corev1.Container{
						{
							Name:            "evictor",
							SecurityContext: r.ModelServerPods.ModelContainerSecurityContext,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "model",
									MountPath: "/models",
									SubPath:   "models",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "model",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: cachePVCName(m, c),
								},
							},
						},
					},
				},
			},
		},
	}

	job.Spec.Template.Spec.Containers[0].Image = r.ModelLoaders.Image
	job.Spec.Template.Spec.Containers[0].Command = []string{"bash", "-c", "rm -rf " + modelCacheDir(m)}

	return job
}

func modelCacheDir(m *kubeaiv1.Model) string {
	return fmt.Sprintf("/models/%s-%s", m.Name, m.UID)
}

func loadCacheJobName(m *kubeaiv1.Model) string {
	return fmt.Sprintf("load-cache-%s", m.Name)
}

func evictCacheJobName(m *kubeaiv1.Model) string {
	return fmt.Sprintf("evict-cache-%s", m.Name)
}

func patchServerCacheVolumes(podSpec *corev1.PodSpec, m *kubeaiv1.Model, c ModelConfig) {
	if m.Spec.CacheProfile == "" {
		return
	}
	podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
		Name: "models",
		VolumeSource: corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: cachePVCName(m, c),
			},
		},
	})
	for i := range podSpec.Containers {
		if podSpec.Containers[i].Name == "server" {
			podSpec.Containers[i].VolumeMounts = append(podSpec.Containers[i].VolumeMounts, corev1.VolumeMount{
				Name:      "models",
				MountPath: modelCacheDir(m),
				SubPath:   strings.TrimPrefix(modelCacheDir(m), "/"),
				ReadOnly:  true,
			})
		}
	}
}
