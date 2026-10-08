package modelcontroller

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	kubeaiv1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	lwsv1 "sigs.k8s.io/lws/api/leaderworkerset/v1"
)

func (r *ModelReconciler) vLLMPodForModel(m *kubeaiv1.Model, c ModelConfig) *corev1.Pod {
	lbs := labelsForModel(m)
	ann := r.annotationsForModel(m)
	if _, ok := ann[kubeaiv1.ModelPodPortAnnotation]; !ok {
		// Set port to 8000 (vLLM) if not overwritten.
		ann[kubeaiv1.ModelPodPortAnnotation] = "8000"
	}

	vllmModelFlag := c.Source.url.ref
	useRunaiStreamer := false
	if m.Spec.CacheProfile != "" {
		vllmModelFlag = modelCacheDir(m)
	} else if c.Source.url.scheme == "s3" {
		vllmModelFlag = c.Source.url.original
		useRunaiStreamer = true
	}
	// The vllmModelFlag can be safely overridden because validation logic ensures
	// that a model with PVC source and cacheProfile won't be admitted.
	if c.Source.url.scheme == "pvc" {
		vllmModelFlag = "/model"
	}

	args := []string{
		"--model=" + vllmModelFlag,
		"--served-model-name=" + m.Name,
	}
	if useRunaiStreamer {
		args = append(args, "--load-format=runai_streamer")
	}
	args = append(args, m.Spec.Args...)

	env := []corev1.EnvVar{}

	if m.Spec.Adapters != nil {
		args = append(args, "--enable-lora")
		env = append(env, corev1.EnvVar{
			// https://docs.vllm.ai/en/latest/features/lora/#dynamically-serving-lora-adapters
			Name:  "VLLM_ALLOW_RUNTIME_LORA_UPDATING",
			Value: "True",
		})
	}

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

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   m.Namespace,
			Labels:      lbs,
			Annotations: ann,
		},
		Spec: corev1.PodSpec{
			NodeSelector:       c.NodeSelector,
			Affinity:           c.Affinity,
			Tolerations:        c.Tolerations,
			SchedulerName:      c.SchedulerName,
			RuntimeClassName:   c.RuntimeClassName,
			PriorityClassName:  m.Spec.PriorityClassName,
			ServiceAccountName: r.ModelServerPods.ModelServiceAccountName,
			SecurityContext:    r.ModelServerPods.ModelPodSecurityContext,
			ImagePullSecrets:   r.ModelServerPods.ImagePullSecrets,
			Containers: []corev1.Container{
				{
					Name:            serverContainerName,
					Image:           c.Image,
					Command:         []string{"python3", "-m", "vllm.entrypoints.openai.api_server"},
					Args:            args,
					Env:             env,
					SecurityContext: r.ModelServerPods.ModelContainerSecurityContext,
					Resources: corev1.ResourceRequirements{
						Requests: c.Requests,
						Limits:   c.Limits,
					},
					Ports: []corev1.ContainerPort{
						{
							ContainerPort: 8000,
							Protocol:      corev1.ProtocolTCP,
							Name:          "http",
						},
					},
					StartupProbe: &corev1.Probe{
						// TODO: Decrease the default and make it configurable.
						// Give the model 3 hours to start up.
						FailureThreshold: 5400,
						PeriodSeconds:    2,
						TimeoutSeconds:   2,
						SuccessThreshold: 1,
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Path: "/health",
								Port: intstr.FromString("http"),
							},
						},
					},
					ReadinessProbe: &corev1.Probe{
						FailureThreshold: 3,
						PeriodSeconds:    10,
						TimeoutSeconds:   2,
						SuccessThreshold: 1,
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Path: "/health",
								Port: intstr.FromString("http"),
							},
						},
					},
					LivenessProbe: &corev1.Probe{
						FailureThreshold: 3,
						PeriodSeconds:    30,
						TimeoutSeconds:   3,
						SuccessThreshold: 1,
						ProbeHandler: corev1.ProbeHandler{
							HTTPGet: &corev1.HTTPGetAction{
								Path: "/health",
								Port: intstr.FromString("http"),
							},
						},
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "dshm",
							MountPath: "/dev/shm",
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "dshm",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{
							Medium: corev1.StorageMediumMemory,
							// TODO: Set size limit
						},
					},
				},
			},
		},
	}

	patchFileVolumes(&pod.Spec, m)
	r.patchServerAdapterLoader(&pod.Spec, m, r.ModelLoaders.Image)
	patchServerCacheVolumes(&pod.Spec, m, c)
	c.Source.modelSourcePodAdditions.applyToPodSpec(&pod.Spec, 0)

	return pod
}

// buildVLLMLeaderWorkerSet constructs a vLLM LeaderWorkerSet manifest for a multi-node model.
func (r *ModelReconciler) buildVLLMLeaderWorkerSet(model *kubeaiv1.Model, c ModelConfig) (*lwsv1.LeaderWorkerSet, error) {
	if c.PodBuilder == nil {
		return nil, fmt.Errorf("no pod builder configured for engine %q", model.Spec.Engine)
	}

	podForModel := c.PodBuilder(model, c)
	if err := applyJSONPatchToPod(r.ModelServerPods.JSONPatches, podForModel); err != nil {
		return nil, err
	}

	lbs := labelsForModel(model)
	ann := map[string]string{
		"kubeai.org/tensor-parallel-size":   strconv.Itoa(c.LWSConfig.TensorParallelSize),
		"kubeai.org/pipeline-parallel-size": strconv.Itoa(c.LWSConfig.PipelineParallelSize),
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
		corev1.EnvVar{Name: "LWS_GROUP_SIZE", Value: strconv.Itoa(c.LWSConfig.PipelineParallelSize)},
	)

	const rayLeaderBootstrap = "bash /vllm-workspace/examples/online_serving/multi-node-serving.sh leader --ray_cluster_size=$(LWS_GROUP_SIZE)"
	vllmEntrypoint := strings.Join(headPod.Spec.Containers[0].Command, " ")
	headPod.Spec.Containers[0].Command = []string{
		"bash", "-c",
		fmt.Sprintf("%s && %s \"$@\"", rayLeaderBootstrap, vllmEntrypoint),
	}

	headPod.Spec.Containers[0].Args = append(headPod.Spec.Containers[0].Args,
		fmt.Sprintf("--tensor-parallel-size=%d", c.LWSConfig.TensorParallelSize),
		fmt.Sprintf("--pipeline-parallel-size=%d", c.LWSConfig.PipelineParallelSize),
		"--distributed-executor-backend=ray",
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
		"bash /vllm-workspace/examples/online_serving/multi-node-serving.sh worker --ray_address=$(LWS_LEADER_ADDRESS)",
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
			Name:        LwsName(model),
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
				Size:          ptr.To(int32(c.LWSConfig.PipelineParallelSize)),
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
