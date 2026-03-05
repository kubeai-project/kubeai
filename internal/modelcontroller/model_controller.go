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
	lwsv1 "sigs.k8s.io/lws/api/leaderworkerset/v1"

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
		if modelConfig.LWSConfig != nil {
			// Delete all LeaderWorkerSets for the Model.
			if err := r.DeleteAllOf(ctx, &lwsv1.LeaderWorkerSet{}, client.InNamespace(model.Namespace), client.MatchingLabels{
				kubeaiv1.PodModelLabel: model.Name,
			}); err != nil && !apierrors.IsNotFound(err) {
				return ctrl.Result{}, fmt.Errorf("deleting all LWS: %w", err)
			}
		} else {
			// Get rid of all Pods for the Model.
			// This should help avoid any issues with cache cleanup.
			if err := r.DeleteAllOf(ctx, &corev1.Pod{}, client.InNamespace(model.Namespace), client.MatchingLabels{
				kubeaiv1.PodModelLabel: model.Name,
			}); err != nil {
				if !apierrors.IsNotFound(err) {
					return ctrl.Result{}, fmt.Errorf("deleting all pods: %w", err)
				}
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
		return ctrl.Result{}, fmt.Errorf("listing all pods: %w", err)
	}

	scaled := false
	defer func() {
		if scaled {
			// Slow things down to wait for caches to sync.
			// This is important because the pod plan has some calculations that
			// assume the cache is up to date.
			// TODO: Use "expectations" instead of a wait - see the ReplicaSet controller.
			time.Sleep(3 * time.Second)
		}
	}()

	// Select the appropriate plan based on whether this is a multi-node (LWS) model.
	var plan executablePlan
	if modelConfig.LWSConfig != nil {
		plan, err = r.calculateLWSPlan(ctx, model, modelConfig)
	} else {
		plan, err = r.calculatePodPlan(allPods, model, modelConfig)
	}
	if err != nil {
		log.Error(err, "calculating plan")
		return ctrl.Result{}, nil
	}

	scaled, err = plan.execute(ctx, r.Client, r.Scheme)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("executing plan: %w", err)
	}

	// Adapter reconciliation only applies to single-node (pod-based) models.
	if pp, ok := plan.(*podPlan); ok {
		if err := r.reconcileAdapters(ctx, pp.toRemain, model.Spec.Adapters); err != nil {
			if errors.Is(err, errReturnEarly) {
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, fmt.Errorf("reconciling adapters: %w", err)
		}
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
		Owns(&lwsv1.LeaderWorkerSet{}).
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
