package modelcontroller

import (
	"sort"

	kubeaiv1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func (r *ModelReconciler) sGLangPodForModel(m *kubeaiv1.Model, c ModelConfig) *corev1.Pod {
	lbs := labelsForModel(m)
	ann := r.annotationsForModel(m)
	if _, ok := ann[kubeaiv1.ModelPodPortAnnotation]; !ok {
		// SGLang defaults to port 30000; the engine pins 8000 below.
		ann[kubeaiv1.ModelPodPortAnnotation] = "8000"
	}

	sglangModelFlag := c.Source.url.ref
	useRunaiStreamer := false
	if m.Spec.CacheProfile != "" {
		sglangModelFlag = modelCacheDir(m)
	} else if c.Source.url.scheme == "s3" {
		sglangModelFlag = c.Source.url.original
		useRunaiStreamer = true
	}
	// The flag can be safely overridden because validation logic ensures
	// that a model with PVC source and cacheProfile won't be admitted.
	if c.Source.url.scheme == "pvc" || c.Source.url.scheme == "oci" {
		sglangModelFlag = "/model"
	}

	args := []string{
		"--model-path=" + sglangModelFlag,
		"--served-model-name=" + m.Name,
		// SGLang binds 127.0.0.1 by default, which the proxy cannot reach.
		"--host=0.0.0.0",
		"--port=8000",
	}
	if useRunaiStreamer {
		args = append(args, "--load-format=runai_streamer")
	}
	args = append(args, sGLangFeatureArgs(m.Spec.Features)...)
	args = append(args, m.Spec.Args...)

	env := []corev1.EnvVar{}
	var envKeys []string
	for key := range m.Spec.Env {
		envKeys = append(envKeys, key)
	}
	sort.Strings(envKeys)
	for _, key := range envKeys {
		env = append(env, corev1.EnvVar{Name: key, Value: m.Spec.Env[key]})
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
					Command:         []string{"python3", "-m", "sglang.launch_server"},
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
						// /health returns 503 until the weights are loaded and
						// the warmup request has completed.
						// Give the model 3 hours to start up.
						FailureThreshold: 5400,
						PeriodSeconds:    2,
						TimeoutSeconds:   2,
						SuccessThreshold: 1,
						ProbeHandler:     sGLangHealthProbe(),
					},
					ReadinessProbe: &corev1.Probe{
						FailureThreshold: 3,
						PeriodSeconds:    10,
						TimeoutSeconds:   2,
						SuccessThreshold: 1,
						ProbeHandler:     sGLangHealthProbe(),
					},
					LivenessProbe: &corev1.Probe{
						FailureThreshold: 3,
						PeriodSeconds:    30,
						TimeoutSeconds:   3,
						SuccessThreshold: 1,
						ProbeHandler:     sGLangHealthProbe(),
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
	// Adapters are restricted to the VLLM engine, so no adapter loader here.
	patchServerCacheVolumes(&pod.Spec, m, c)
	c.Source.modelSourcePodAdditions.applyToPodSpec(&pod.Spec, 0)

	return pod
}

// sGLangHealthProbe targets /health rather than /health_generate: the latter
// runs a token generation on every probe.
func sGLangHealthProbe() corev1.ProbeHandler {
	return corev1.ProbeHandler{
		HTTPGet: &corev1.HTTPGetAction{
			Path: "/health",
			Port: intstr.FromString("http"),
		},
	}
}

// sGLangFeatureArgs maps Model features onto the flags that put SGLang into
// embedding mode. SGLang only auto-detects this for dedicated embedding
// architectures, so serving a CausalLM for embeddings or reranking needs the
// flag set explicitly.
func sGLangFeatureArgs(features []kubeaiv1.ModelFeature) []string {
	for _, f := range features {
		switch f {
		case kubeaiv1.ModelFeatureTextEmbedding, kubeaiv1.ModelFeatureReranking:
			return []string{"--is-embedding"}
		}
	}
	return nil
}
