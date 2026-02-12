/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package modelcontroller

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/client-go/rest"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kubeaiv1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/kubeai-project/kubeai/internal/config"
	"github.com/kubeai-project/kubeai/internal/k8sutils"
	"github.com/kubeai-project/kubeai/internal/vllmclient"
	corev1 "k8s.io/api/core/v1"
)

const (
	modelReconcilerName = "kubeai-model-controller"
	serverContainerName = "server"
	// Model files ConfigMap volume name
	modelFilesVolumeName = "model-files"
)

// ModelReconciler reconciles a Model object
type ModelReconciler struct {
	client.Client
	RESTConfig              *rest.Config
	PodRESTClient           rest.Interface
	Scheme                  *runtime.Scheme
	VLLMClient              *vllmclient.Client
	Namespace               string
	AllowPodAddressOverride bool
	SecretNames             config.SecretNames
	ResourceProfiles        map[string]config.ResourceProfile
	CacheProfiles           map[string]config.CacheProfile
	ModelServers            config.ModelServers
	ModelServerPods         config.ModelServerPods
	ModelLoaders            config.ModelLoading
	ModelRollouts           config.ModelRollouts
}

func (r *ModelReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, resErr error) {
	log := log.FromContext(ctx)
	log.Info("Reconciling Model")

	model := &kubeaiv1.Model{}
	if err := r.Get(ctx, req.NamespacedName, model); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	status0 := model.Status.DeepCopy()

	defer func() {
		if !reflect.DeepEqual(status0, model.Status) && model.DeletionTimestamp == nil {
			if err := r.Status().Update(ctx, model); err != nil {
				resErr = errors.Join(resErr, err)
			}
		}
	}()

	// Ensure ConfigMap for model files exists and is up to date
	if err := r.ensureModelFilesConfigMap(ctx, model); err != nil {
		log.Error(err, "Failed to ensure model files ConfigMap")
		return ctrl.Result{}, err
	}

	// Apply self labels based on features so that we can easily filter models.
	shouldUpdate := r.applySelfLabels(model)
	// Apply replica bounds to handle cases where min/max replicas were updated but a scale event was not triggered.
	if !model.Spec.AutoscalingDisabled {
		shouldUpdate = r.applyAutoscalingReplicaBounds(model) || shouldUpdate
	}
	if shouldUpdate {
		if err := r.Update(ctx, model, k8sutils.DefaultUpdateOptions()); err != nil {
			return ctrl.Result{}, fmt.Errorf("updating model: %w", err)
		}
	}

	modelConfig, err := r.getModelConfig(model)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("getting model profile: %w", err)
	}

	if model.DeletionTimestamp != nil {
		// Get rid of all Pods for the Model.
		// This should help avoid any issues with cache cleanup.
		if err := r.DeleteAllOf(ctx, &corev1.Pod{}, client.InNamespace(model.Namespace), client.MatchingLabels{
			kubeaiv1.PodModelLabel: model.Name,
		}); err != nil {
			if !apierrors.IsNotFound(err) {
				return ctrl.Result{}, fmt.Errorf("deleting all pods: %w", err)
			}
		}
		if model.Spec.CacheProfile != "" {
			if err := r.finalizeCache(ctx, model, modelConfig); err != nil {
				if errors.Is(err, errReturnEarly) {
					return ctrl.Result{}, nil
				} else {
					return ctrl.Result{}, fmt.Errorf("finalizing cache: %w", err)
				}
			}
		}

		return ctrl.Result{}, nil
	}

	if model.Spec.CacheProfile != "" {
		cacheRes, err := r.reconcileCache(ctx, model, modelConfig)
		if err != nil {
			if errors.Is(err, errReturnEarly) {
				return cacheRes, nil
			}
			return cacheRes, fmt.Errorf("reconciling cache: %w", err)
		}
		if !res.IsZero() {
			return cacheRes, nil
		}
	}

	allPods := &corev1.PodList{}
	if err := r.List(ctx, allPods, client.InNamespace(model.Namespace), client.MatchingLabels{
		kubeaiv1.PodModelLabel: model.Name,
	}); err != nil {
		return ctrl.Result{}, fmt.Errorf("listing all node pools: %w", err)
	}

	// Summarize all pods.
	var readyPods int32
	for _, pod := range allPods.Items {
		if k8sutils.PodIsReady(&pod) {
			readyPods++
		}
	}
	model.Status.Replicas.All = int32(len(allPods.Items))
	model.Status.Replicas.Ready = readyPods

	scaled := false
	defer func() {
		if scaled {
			// Slow things down to wait for caches to sync.
			// This is important because the pod plan has some calculations that
			// assume the cache is up to date.
			// TODO: Use "epectations" instead of a wait - see the ReplicaSet controller.
			time.Sleep(3 * time.Second)
		}
	}()

	plan, err := r.calculatePodPlan(allPods, model, modelConfig)
	if err != nil {
		log.Error(err, "Failed to calculate pod plan")
		return ctrl.Result{}, nil
	}

	if plan.containsActions() {
		var err error
		scaled, err = plan.execute(ctx, r.Client, r.Scheme)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("executing pod plan: %w", err)
		}
	}

	if err := r.reconcileAdapters(ctx, plan.toRemain, model.Spec.Adapters); err != nil {
		if errors.Is(err, errReturnEarly) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("reconciling adapters: %w", err)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ModelReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// TODO: Set Model concurrency. Pod rollouts can be slow.
	return ctrl.NewControllerManagedBy(mgr).
		For(&kubeaiv1.Model{}).
		Owns(&corev1.Pod{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&batchv1.Job{}).
		Complete(r)
}

var errReturnEarly = fmt.Errorf("return early")

const (
	appKubernetesIOName = "app.kubernetes.io/name"
)

func labelsForModel(m *kubeaiv1.Model) map[string]string {
	engineLowerCase := strings.ToLower(m.Spec.Engine)
	return map[string]string{
		"app":                          "model",
		"model":                        m.Name,
		appKubernetesIOName:            engineLowerCase,
		"app.kubernetes.io/instance":   engineLowerCase + "-" + m.Name,
		"app.kubernetes.io/managed-by": "kubeai",
	}
}

func (r *ModelReconciler) annotationsForModel(m *kubeaiv1.Model) map[string]string {
	ann := map[string]string{}

	if modelAnn := m.GetAnnotations(); modelAnn != nil {
		var keys []string
		if r.AllowPodAddressOverride {
			keys = append(keys,
				kubeaiv1.ModelPodIPAnnotation,
				kubeaiv1.ModelPodPortAnnotation,
			)
		}
		// Copy over relevant model annotations.
		for _, key := range keys {
			if val, ok := modelAnn[key]; ok {
				ann[key] = val
			}
		}
	}

	return ann
}

type ModelConfig struct {
	config.CacheProfile
	config.ResourceProfile
	Image  string
	Source modelSource
}

func (r *ModelReconciler) getModelConfig(model *kubeaiv1.Model) (ModelConfig, error) {
	var result ModelConfig

	src, err := r.parseModelSource(model.Spec.URL)
	if err != nil {
		return result, fmt.Errorf("parsing model source: %w", err)
	}
	result.Source = src

	if model.Spec.CacheProfile != "" {
		cacheProfile, ok := r.CacheProfiles[model.Spec.CacheProfile]
		if !ok {
			return result, fmt.Errorf("cache profile not found: %q", model.Spec.CacheProfile)
		}
		result.CacheProfile = cacheProfile
	}

	split := strings.Split(model.Spec.ResourceProfile, ":")
	if len(split) != 2 {
		return result, fmt.Errorf("invalid resource profile: %q, should match <name>:<multiple>, example: nvidia-gpu-l4:2", model.Spec.ResourceProfile)
	}
	name := split[0]
	multiple, err := strconv.Atoi(split[1])
	if err != nil {
		return result, fmt.Errorf("invalid multiple in resource profile multiple: %q: %w", split[1], err)
	}

	profile, ok := r.ResourceProfiles[name]
	if !ok {
		return result, fmt.Errorf("resource profile not found: %q", name)
	}

	requests := make(corev1.ResourceList)
	for key, quantity := range profile.Requests {
		q := quantity.DeepCopy()
		q.Mul(int64(multiple))
		requests[key] = q
	}

	limits := make(corev1.ResourceList)
	for key, quantity := range profile.Limits {
		q := quantity.DeepCopy()
		q.Mul(int64(multiple))
		limits[key] = q
	}

	result.ResourceProfile = profile
	// Apply the multiplied requests and limits to the profile.
	result.Requests = requests
	result.Limits = limits

	if model.Spec.EnvFrom != nil {
		result.Source.modelSourcePodAdditions.envFrom = model.Spec.EnvFrom
	}

	image, err := r.lookupServerImage(model, profile)
	if err != nil {
		return result, fmt.Errorf("looking up server image: %w", err)
	}
	result.Image = image

	return result, nil
}

func (r *ModelReconciler) lookupServerImage(model *kubeaiv1.Model, profile config.ResourceProfile) (string, error) {
	if model.Spec.Image != "" {
		return model.Spec.Image, nil
	}

	var serverImgs map[string]string
	switch model.Spec.Engine {
	case kubeaiv1.OLlamaEngine:
		serverImgs = r.ModelServers.OLlama.Images
	case kubeaiv1.FasterWhisperEngine:
		serverImgs = r.ModelServers.FasterWhisper.Images
	case kubeaiv1.InfinityEngine:
		serverImgs = r.ModelServers.Infinity.Images
	default:
		serverImgs = r.ModelServers.VLLM.Images
	}

	// If no image name is provided for a profile, use the default image name.
	const defaultImageName = "default"
	imageName := defaultImageName
	if profile.ImageName != "" {
		imageName = profile.ImageName
	}

	if img, ok := serverImgs[imageName]; ok {
		return img, nil
	}

	// If the specific profile image name does not exist, use the default image name.
	if img, ok := serverImgs[defaultImageName]; ok {
		return img, nil
	} else {
		return "", fmt.Errorf("missing default server image")
	}
}

func (r *ModelReconciler) applyAutoscalingReplicaBounds(model *kubeaiv1.Model) bool {
	min := model.Spec.MinReplicas
	max := model.Spec.MaxReplicas

	if model.Spec.Replicas == nil || *model.Spec.Replicas < min {
		model.Spec.Replicas = ptr.To(min)
		return true
	}

	if max != nil && *model.Spec.Replicas > *max {
		model.Spec.Replicas = ptr.To(*max)
		return true
	}

	return false
}

func (r *ModelReconciler) applySelfLabels(model *kubeaiv1.Model) bool {
	modelFeaturesMap := make(map[kubeaiv1.ModelFeature]struct{}, len(model.Spec.Features))
	for _, f := range model.Spec.Features {
		modelFeaturesMap[f] = struct{}{}
	}

	if model.GetLabels() == nil {
		model.SetLabels(map[string]string{})
	}

	var changed bool

	// Delete non-matching feature labels.
	for key := range model.GetLabels() {
		if strings.HasPrefix(key, kubeaiv1.ModelFeatureLabelDomain) {
			feat := strings.TrimPrefix(key, kubeaiv1.ModelFeatureLabelDomain+"/")
			if _, ok := modelFeaturesMap[kubeaiv1.ModelFeature(feat)]; !ok {
				delete(model.GetLabels(), key)
				changed = true
			}
		}
	}

	// Add missing feature labels.
	for feat := range modelFeaturesMap {
		labelKey := fmt.Sprintf("%s/%s", kubeaiv1.ModelFeatureLabelDomain, feat)
		if _, ok := model.GetLabels()[labelKey]; !ok {
			model.GetLabels()[labelKey] = "true"
			changed = true
		}
	}

	return changed
}
