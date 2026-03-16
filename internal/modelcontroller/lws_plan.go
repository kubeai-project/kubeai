package modelcontroller

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	kubeaiv1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/kubeai-project/kubeai/internal/k8sutils"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
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
func (r *ModelReconciler) calculateLWSPlan(ctx context.Context, model *kubeaiv1.Model, cfg ModelConfig) (executablePlan, error) {
	if cfg.LWSConfig.PipelineParallelSize < 2 {
		return nil, errors.New("LWS group size (pipeline-parallel) must be at least 2")
	}

	plan := &lwsPlan{model: model}

	lws := new(lwsv1.LeaderWorkerSet)
	lwsKey := apitypes.NamespacedName{Name: lwsName(model), Namespace: model.Namespace}
	if err := r.Client.Get(ctx, lwsKey, lws); err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("getting LeaderWorkerSet: %w", err)
		}
		// LWS does not exist yet — build one.
		newLWS, err := r.buildLeaderWorkerSet(model, cfg)
		if err != nil {
			return nil, fmt.Errorf("building LeaderWorkerSet: %w", err)
		}
		plan.toCreate = newLWS
		// Status is zero since nothing is running yet.
		model.Status.Replicas.All = 0
		model.Status.Replicas.Ready = 0
		return plan, nil
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
func lwsName(model *kubeaiv1.Model) string {
	return strings.ReplaceAll(model.Name, ".", "-")
}

// lwsPlan implements executablePlan for multi-node (LeaderWorkerSet) models.
type lwsPlan struct {
	model    *kubeaiv1.Model
	toCreate *lwsv1.LeaderWorkerSet // nil if no creation needed
	toScale  *lwsv1.LeaderWorkerSet // nil if no scaling needed
	toDelete *lwsv1.LeaderWorkerSet // nil if no deletion needed
	details  []string
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
	if cfg.PodBuilder == nil {
		return nil, fmt.Errorf("no pod builder configured for engine %q", model.Spec.Engine)
	}

	podForModel := cfg.PodBuilder(model, cfg)
	if err := applyJSONPatchToPod(r.ModelServerPods.JSONPatches, podForModel); err != nil {
		return nil, err
	}

	lbs := labelsForModel(model)
	ann := map[string]string{
		"kubeai.org/tensor-parallel-size":   strconv.Itoa(cfg.LWSConfig.TensorParallelSize),
		"kubeai.org/pipeline-parallel-size": strconv.Itoa(cfg.LWSConfig.PipelineParallelSize),
	}

	// --- Head pod ---
	headPod := podForModel.DeepCopy()
	headPod.ObjectMeta.Labels[LabelGroupRole] = GroupRoleHead

	headPod.Spec.Containers[0].Ports = append(headPod.Spec.Containers[0].Ports, corev1.ContainerPort{
		Name:          rayPortName,
		ContainerPort: int32(rayPort),
		Protocol:      corev1.ProtocolTCP,
	})

	headPod.Spec.Containers[0].Env = append(headPod.Spec.Containers[0].Env,
		corev1.EnvVar{Name: "LWS_GROUP_SIZE", Value: strconv.Itoa(cfg.LWSConfig.PipelineParallelSize)},
	)

	headPod.Spec.Containers[0].Args = append(headPod.Spec.Containers[0].Args,
		fmt.Sprintf("--tensor-parallel-size=%d", cfg.LWSConfig.TensorParallelSize),
		fmt.Sprintf("--pipeline-parallel-size=%d", cfg.LWSConfig.PipelineParallelSize),
	)

	// Head uses HTTP health probes on the vLLM server (already set by vLLMPodForModel).

	// --- Worker pod ---
	workerPod := podForModel.DeepCopy()
	workerPod.ObjectMeta.Labels[LabelGroupRole] = GroupRoleWorker
	// Remove the model label from workers — only head pods should receive traffic.
	delete(workerPod.ObjectMeta.Labels, "model")
	delete(workerPod.ObjectMeta.Labels, kubeaiv1.PodModelLabel)

	// Workers don't serve the model API — they join the Ray cluster as workers.
	// Replace the vLLM command with a ray worker start.
	workerPod.Spec.Containers[0].Command = []string{
		"bash", "-c",
		"ray start --address=$(LWS_LEADER_ADDRESS):6379 --block",
	}
	workerPod.Spec.Containers[0].Args = nil

	workerPod.Spec.Containers[0].Env = append(workerPod.Spec.Containers[0].Env,
		corev1.EnvVar{
			Name: "LWS_LEADER_ADDRESS",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: fmt.Sprintf("metadata.annotations['%s']", lwsv1.LeaderPodNameAnnotationKey),
				},
			},
		},
	)

	workerPod.Spec.Containers[0].Ports = []corev1.ContainerPort{{
		Name:          rayPortName,
		ContainerPort: int32(rayPort),
		Protocol:      corev1.ProtocolTCP,
	}}

	rayProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{"ray", "status"},
			},
		},
		InitialDelaySeconds: 30,
		PeriodSeconds:       10,
		TimeoutSeconds:      5,
		FailureThreshold:    3,
	}
	workerStartupProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			Exec: &corev1.ExecAction{
				Command: []string{"ray", "status"},
			},
		},
		InitialDelaySeconds: 10,
		PeriodSeconds:       5,
		TimeoutSeconds:      5,
		FailureThreshold:    20,
	}
	workerPod.Spec.Containers[0].LivenessProbe = rayProbe
	workerPod.Spec.Containers[0].ReadinessProbe = rayProbe
	workerPod.Spec.Containers[0].StartupProbe = workerStartupProbe

	lws := &lwsv1.LeaderWorkerSet{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "leaderworkerset.x-k8s.io/v1",
			Kind:       "LeaderWorkerSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        lwsName(model),
			Namespace:   model.Namespace,
			Labels:      lbs,
			Annotations: ann,
		},
		Spec: lwsv1.LeaderWorkerSetSpec{
			Replicas: model.Spec.Replicas,
			RolloutStrategy: lwsv1.RolloutStrategy{
				Type: lwsv1.RollingUpdateStrategyType,
				RollingUpdateConfiguration: &lwsv1.RollingUpdateConfiguration{
					MaxUnavailable: intstr.IntOrString{IntVal: 1},
					MaxSurge:       intstr.IntOrString{IntVal: 0},
				},
			},
			StartupPolicy: lwsv1.LeaderCreatedStartupPolicy,
			NetworkConfig: &lwsv1.NetworkConfig{
				SubdomainPolicy: ptr.To(lwsv1.SubdomainUniquePerReplica),
			},
			LeaderWorkerTemplate: lwsv1.LeaderWorkerTemplate{
				RestartPolicy: lwsv1.NoneRestartPolicy,
				Size:          ptr.To(int32(cfg.LWSConfig.PipelineParallelSize)),
				LeaderTemplate: &corev1.PodTemplateSpec{
					ObjectMeta: headPod.ObjectMeta,
					Spec:       headPod.Spec,
				},
				WorkerTemplate: corev1.PodTemplateSpec{
					ObjectMeta: workerPod.ObjectMeta,
					Spec:       workerPod.Spec,
				},
			},
		},
	}

	return lws, nil
}
