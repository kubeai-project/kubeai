package modelcontroller

import (
	"context"
	"crypto"
	"errors"
	"fmt"
	"math"
	"strings"

	kubeaiv1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/kubeai-project/kubeai/internal/k8sutils"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	lwsv1 "sigs.k8s.io/lws/api/leaderworkerset/v1"
)

const (
	// LabelGroupRole distinguishes head and worker pods in a multi-node group.
	LabelGroupRole  = "kubeai.org/group-role"
	GroupRoleHead   = "head"
	GroupRoleWorker = "worker"
	rayPort         = 6379
	rayPortName     = "ray"
)

// calculateLWSPlan looks up the existing LeaderWorkerSet for the model and
// returns a plan to create, scale, or leave it unchanged.
func (r *ModelReconciler) calculateLWSPlan(ctx context.Context, model *kubeaiv1.Model, cfg ModelConfig) (*lwsPlan, error) {
	if cfg.LWSConfig.PipelineParallelSize < 2 {
		return nil, errors.New("LWS group size (pipeline-parallel) must be at least 2")
	}

	plan := &lwsPlan{model: model}

	newLWS, err := r.buildLeaderWorkerSet(model, cfg)
	if err != nil {
		return nil, fmt.Errorf("building LeaderWorkerSet: %w", err)
	}

	leaderExpectedHash := k8sutils.PodHash(newLWS.Spec.LeaderWorkerTemplate.LeaderTemplate.Spec)
	k8sutils.SetLabel(newLWS, kubeaiv1.LeaderHashLabel, leaderExpectedHash)

	workerExpectedHash := k8sutils.PodHash(newLWS.Spec.LeaderWorkerTemplate.WorkerTemplate.Spec)
	k8sutils.SetLabel(newLWS, kubeaiv1.WorkerHashLabel, workerExpectedHash)

	lws := new(lwsv1.LeaderWorkerSet)
	lwsKey := apitypes.NamespacedName{Name: LwsName(model), Namespace: model.Namespace}
	if err := r.Client.Get(ctx, lwsKey, lws); err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("getting LeaderWorkerSet: %w", err)
		}
		plan.toCreate = newLWS
		// Status is zero since nothing is running yet.
		model.Status.Replicas.All = 0
		model.Status.Replicas.Ready = 0
		return plan, nil
	}

	if k8sutils.GetLabel(lws, kubeaiv1.LeaderHashLabel) != leaderExpectedHash ||
		k8sutils.GetLabel(lws, kubeaiv1.WorkerHashLabel) != workerExpectedHash {
		upgrade := lws.DeepCopy()
		upgrade.Spec = newLWS.Spec
		k8sutils.SetLabel(upgrade, kubeaiv1.LeaderHashLabel, leaderExpectedHash)
		k8sutils.SetLabel(upgrade, kubeaiv1.WorkerHashLabel, workerExpectedHash)
		plan.toUpgrade = upgrade
		plan.details = append(plan.details, "LWS spec changed, rolling update required")
	} else {
		plan.details = append(plan.details, "No changes to LWS spec, no rollout needed")
	}

	// LWS exists — update model status from LWS status.
	model.Status.Replicas.All = lws.Status.Replicas
	model.Status.Replicas.Ready = lws.Status.ReadyReplicas

	// Determine if scaling is needed.
	var desiredReplicas int32
	if model.Spec.Replicas != nil {
		desiredReplicas = *model.Spec.Replicas
	}

	observedReplicas := int32(0)
	if lws.Spec.Replicas != nil {
		observedReplicas = *lws.Spec.Replicas
	}

	replicaDiff := observedReplicas - desiredReplicas
	replicaDiffAbs := int32(math.Abs(float64(replicaDiff)))
	switch {
	case replicaDiff < 0:
		plan.details = append(plan.details, fmt.Sprintf("Scaling up from %d to %d groups (+%d)", observedReplicas, desiredReplicas, replicaDiffAbs))
		plan.toScale = lws.DeepCopy()
		plan.toScale.Spec.Replicas = ptr.To(desiredReplicas)
	case replicaDiff > 0:
		plan.details = append(plan.details, fmt.Sprintf("Scaling down from %d to %d groups (-%d)", observedReplicas, desiredReplicas, replicaDiffAbs))
		plan.toScale = lws.DeepCopy()
		plan.toScale.Spec.Replicas = ptr.To(desiredReplicas)
	}

	return plan, nil
}

// lwsName returns a DNS-compatible name for the LWS resource.
func LwsName(model *kubeaiv1.Model) string {
	sha256 := fmt.Sprintf("%x", crypto.SHA256.New().Sum([]byte(model.Name)))[:8]
	lwsName := fmt.Sprintf("model-%s-%s", strings.ReplaceAll(model.Name, ".", "-"), sha256)
	return lwsName
}

// lwsPlan implements executablePlan for multi-node (LeaderWorkerSet) models.
type lwsPlan struct {
	model     *kubeaiv1.Model
	toCreate  *lwsv1.LeaderWorkerSet // nil if no creation needed
	toUpgrade *lwsv1.LeaderWorkerSet // nil if no upgrade needed
	toScale   *lwsv1.LeaderWorkerSet // nil if no scaling needed
	toDelete  *lwsv1.LeaderWorkerSet // nil if no deletion needed
	details   []string
}

func (lp *lwsPlan) execute(ctx context.Context, k8sClient client.Client, scheme *runtime.Scheme) (bool, error) {
	logger := log.FromContext(ctx)
	if len(lp.details) > 0 {
		logger.Info("Executing LWS plan", "modelName", lp.model.Name, "details", strings.Join(lp.details, ", "))
	}

	var scaled bool

	if lp.toDelete != nil {
		logger.Info("Deleting LeaderWorkerSet", "name", lp.toDelete.Name)
		if err := k8sClient.Delete(ctx, lp.toDelete); err != nil {
			if !apierrors.IsNotFound(err) {
				return false, fmt.Errorf("deleting LeaderWorkerSet: %w", err)
			}
			logger.Info("LeaderWorkerSet already deleted", "name", lp.toDelete.Name)
		}
		scaled = true
	}

	if lp.toCreate != nil {
		logger.Info("Creating LeaderWorkerSet", "name", lp.toCreate.Name)
		if err := ctrl.SetControllerReference(lp.model, lp.toCreate, scheme); err != nil {
			return false, fmt.Errorf("setting controller reference for LeaderWorkerSet: %w", err)
		}
		if err := k8sClient.Create(ctx, lp.toCreate, k8sutils.DefaultCreateOptions()); err != nil {
			if apierrors.IsAlreadyExists(err) {
				logger.Info("LeaderWorkerSet already exists", "name", lp.toCreate.Name)
			} else {
				return false, fmt.Errorf("creating LeaderWorkerSet: %w", err)
			}
		}
		scaled = true
	}

	if lp.toUpgrade != nil {
		logger.Info("Upgrading LeaderWorkerSet", "name", lp.toUpgrade.Name)
		if err := ctrl.SetControllerReference(lp.model, lp.toUpgrade, scheme); err != nil {
			return false, fmt.Errorf("setting controller reference for LeaderWorkerSet: %w", err)
		}
		if err := k8sClient.Update(ctx, lp.toUpgrade, k8sutils.DefaultUpdateOptions()); err != nil {
			return false, fmt.Errorf("upgrading LeaderWorkerSet: %w", err)
		}
		scaled = true
	}

	if lp.toScale != nil {
		logger.Info("Scaling LeaderWorkerSet", "name", lp.toScale.Name, "replicas", *lp.toScale.Spec.Replicas)
		scale := &autoscalingv1.Scale{
			Spec: autoscalingv1.ScaleSpec{Replicas: *lp.toScale.Spec.Replicas},
		}
		if err := k8sClient.SubResource("scale").Update(ctx, lp.toScale, client.WithSubResourceBody(scale)); err != nil {
			return false, fmt.Errorf("scaling LeaderWorkerSet: %w", err)
		}
		scaled = true
	}

	return scaled, nil
}

// buildLeaderWorkerSet constructs a complete LeaderWorkerSet manifest for a multi-node model.
func (r *ModelReconciler) buildLeaderWorkerSet(model *kubeaiv1.Model, cfg ModelConfig) (*lwsv1.LeaderWorkerSet, error) {
	if cfg.LWSBuilder == nil {
		return nil, fmt.Errorf("no LWS builder configured for engine %q", model.Spec.Engine)
	}
	return cfg.LWSBuilder(model, cfg)
}
