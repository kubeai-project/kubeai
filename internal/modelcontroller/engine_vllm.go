package modelcontroller

import (
	"sort"

	kubeaiv1 "github.com/substratusai/kubeai/api/k8s/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
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
	} else if c.Source.url.scheme == "gs" {
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
			// https://docs.vllm.ai/en/latest/models/lora.html#dynamically-serving-lora-adapters
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
