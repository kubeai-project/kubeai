package modelcontroller

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	kubeaiv1 "github.com/substratusai/kubeai/api/k8s/v1"
	"github.com/substratusai/kubeai/internal/k8sutils"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// calculatePodPlan calculates the Pod plan for the given Model.
// It assumes the list of Pods represents an accurate snapshot of the current state.
// It returns a Pod plan that contains Pods to create and delete.
// If a rollout is required, it will return a Pod plan that:
// - Adds a surge Pod
// - Recreates any out-of-date Pod that is not Ready immediately
// - Waits for all Pods to be Ready before recreating any out-of-date Pods that are Ready
func (r *ModelReconciler) calculatePodPlan(allPods *corev1.PodList, model *kubeaiv1.Model, modelConfig ModelConfig) (*podPlan, error) {
	var podForModel *corev1.Pod

	switch model.Spec.Engine {
	case kubeaiv1.OLlamaEngine:
		podForModel = r.oLlamaPodForModel(model, modelConfig)
	case kubeaiv1.FasterWhisperEngine:
		podForModel = r.fasterWhisperPodForModel(model, modelConfig)
	case kubeaiv1.InfinityEngine:
		podForModel = r.infinityPodForModel(model, modelConfig)
	default:
		podForModel = r.vLLMPodForModel(model, modelConfig)
	}

	if err := applyJSONPatchToPod(r.ModelServerPods.JSONPatches, podForModel); err != nil {
		return nil, err
	}

	if err := applyModelJSONPatchToPod(model.Spec.JSONPatches, podForModel); err != nil {
		return nil, err
	}

	expectedHash := k8sutils.PodHash(podForModel.Spec)
	podForModel.GenerateName = fmt.Sprintf("model-%s-%s-", model.Name, expectedHash)
	k8sutils.SetLabel(podForModel, kubeaiv1.PodHashLabel, expectedHash)

	var (
		readyAll  int
		outOfDate []corev1.Pod
		remainder = make(map[string]*corev1.Pod)
	)

	podKey := func(p corev1.Pod) string {
		return p.Namespace + "/" + p.Name
	}

	sortPodsByDeletionOrder(allPods.Items, expectedHash)

	for _, p := range allPods.Items {
		remainder[podKey(p)] = &p

		upToDate := k8sutils.GetLabel(&p, kubeaiv1.PodHashLabel) == expectedHash

		if k8sutils.PodIsReady(&p) {
			readyAll++
		}

		if !upToDate {
			outOfDate = append(outOfDate, p)
		}
	}

	var (
		details  []string
		toCreate []*corev1.Pod
		toDelete []*corev1.Pod
	)
	appendToDelete := func(p corev1.Pod) {
		delete(remainder, podKey(p))
		toDelete = append(toDelete, &p)
	}

	var desiredReplicas int32
	// NOTE: Replicas could be nil if autoscaling is disabled.
	if model.Spec.Replicas != nil {
		desiredReplicas = *model.Spec.Replicas
	}
	if len(outOfDate) > 0 {
		desiredReplicas += r.ModelRollouts.Surge
	}
	observedReplicas := int32(len(allPods.Items))
	replicaDiff := observedReplicas - desiredReplicas
	replicaDiffAbs := int32(math.Abs(float64(replicaDiff)))

	switch {
	case replicaDiff == 0:
		// At correct scale.
	case replicaDiff < 0:
		// Create Pods.
		details = append(details, fmt.Sprintf("Creating %d Pods", replicaDiffAbs))
		for i := int32(0); i < replicaDiffAbs; i++ {
			toCreate = append(toCreate, podForModel.DeepCopy())
		}
	case replicaDiff > 0:
		// Delete Pods.
		details = append(details, fmt.Sprintf("Deleting %d Pods", replicaDiffAbs))
		toDeleteCount := replicaDiffAbs
		for _, pod := range allPods.Items {
			if toDeleteCount == 0 {
				break
			}
			appendToDelete(pod)
			toDeleteCount--
		}
	}

	var recreated int
	for _, pod := range outOfDate {
		if !k8sutils.PodIsReady(&pod) {
			details = append(details, fmt.Sprintf("Out-of-date Pod %q is not ready, immediately recreating", pod.Name))
			appendToDelete(pod)
			// Avoid recreating the surge Pod when rollout is complete.
			if recreated < len(outOfDate)-int(r.ModelRollouts.Surge) {
				toCreate = append(toCreate, podForModel.DeepCopy())
				recreated++
			}
			continue
		}
		if readyAll == int(desiredReplicas) {
			details = append(details, fmt.Sprintf("All Pods ready, recreating out-of-date Pod %q", pod.Name))
			appendToDelete(pod)
			// Avoid recreating the surge Pod when rollout is complete.
			if recreated < len(outOfDate)-int(r.ModelRollouts.Surge) {
				toCreate = append(toCreate, podForModel.DeepCopy())
				recreated++
			}
			break
		}
	}

	toRemain := make([]*corev1.Pod, 0, len(remainder))
	for _, pod := range remainder {
		toRemain = append(toRemain, pod)
	}

	return &podPlan{
		model:    model,
		toCreate: toCreate,
		toDelete: toDelete,
		toRemain: toRemain,
		details:  details,
	}, nil
}

type podPlan struct {
	model    *kubeaiv1.Model
	toCreate []*corev1.Pod
	toDelete []*corev1.Pod
	toRemain []*corev1.Pod
	details  []string
}

func (pp *podPlan) containsActions() bool {
	return len(pp.toCreate) > 0 || len(pp.toDelete) > 0
}

// execute returns true if a Pod was created or deleted.
func (pp *podPlan) execute(ctx context.Context, client client.Client, scheme *runtime.Scheme) (bool, error) {
	log := log.FromContext(ctx)

	detailsCSV := strings.Join(pp.details, ", ")
	log.Info("Executing Pod plan", "modelName", pp.model.Name, "details", detailsCSV)

	var changed bool

	// Delete before create to avoid unnecessary Node scale-ups.
	for _, pod := range pp.toDelete {
		if err := client.Delete(ctx, &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: pod.Namespace,
				Name:      pod.Name,
			},
		}); err != nil {
			if apierrors.IsNotFound(err) {
				log.Info("Pod already deleted", "podName", pod.Name)
			} else {
				return changed, fmt.Errorf("deleting pod: %w", err)
			}
		}
		changed = true
	}

	for _, pod := range pp.toCreate {
		if err := ctrl.SetControllerReference(pp.model, pod, scheme); err != nil {
			return changed, fmt.Errorf("setting controller reference: %w", err)
		}
		if err := client.Create(ctx, pod, k8sutils.DefaultCreateOptions()); err != nil {
			if apierrors.IsAlreadyExists(err) {
				log.Info("Pod already exists", "podName", pod.Name)
			} else {
				return changed, fmt.Errorf("creating pod: %w", err)
			}
		}
		changed = true
	}

	return changed, nil
}

// sortPodsByDeletionOrder ensures Pods that are to be deleted/recreated
// first are lower index.
func sortPodsByDeletionOrder(pods []corev1.Pod, expectedHash string) {
	sort.SliceStable(pods, func(i, j int) bool {
		// Not ready Pods should be deleted first.
		iReady := k8sutils.PodIsReady(&pods[i])
		jReady := k8sutils.PodIsReady(&pods[j])
		if iReady != jReady {
			return !iReady
		}

		// Unscheduled Pods should be deleted first.
		iScheduled := k8sutils.PodIsScheduled(&pods[i])
		jScheduled := k8sutils.PodIsScheduled(&pods[j])
		if iScheduled != jScheduled {
			return !iScheduled
		}

		// Delete Pods that are from older hash first
		iHash := k8sutils.GetLabel(&pods[i], kubeaiv1.PodHashLabel)
		jHash := k8sutils.GetLabel(&pods[j], kubeaiv1.PodHashLabel)
		if iHash != jHash {
			return iHash != expectedHash
		}

		// Younger Pods should be deleted first.
		iCreationTime := pods[i].CreationTimestamp.Time
		jCreationTime := pods[j].CreationTimestamp.Time
		return iCreationTime.After(jCreationTime)
	})
}
