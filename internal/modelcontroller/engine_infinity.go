package modelcontroller

import (
	"sort"

	kubeaiv1 "github.com/substratusai/kubeai/api/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

func (r *ModelReconciler) infinityPodForModel(m *kubeaiv1.Model, c ModelConfig) *corev1.Pod {
	lbs := labelsForModel(m)
	ann := r.annotationsForModel(m)

	args := []string{
		"v2",
	}
	args = append(args, m.Spec.Args...)

	if _, ok := ann[kubeaiv1.ModelPodPortAnnotation]; !ok {
		ann[kubeaiv1.ModelPodPortAnnotation] = "8000"
	}

	infinityModelID := c.Source.huggingface.repo
	if m.Spec.CacheProfile != "" {
		// TODO: Verify loading from dir works.
		infinityModelID = modelCacheDir(m)
	}

	env := []corev1.EnvVar{
		{
			Name: "INFINITY_MODEL_ID",
			// TODO: infinity supports multiple models, separate by comma.
			Value: infinityModelID,
		},
		{
			Name:  "INFINITY_SERVED_MODEL_NAME",
			Value: m.Name,
		},
		{
			Name:  "INFINITY_URL_PREFIX",
			Value: "/v1",
		},
		{
			Name: "INFINITY_ENGINE",
			// TODO: switch between optimum backend (cpu), nvidia/amd (torch), inf2 (inferentia) based on what is available.
			Value: "torch",
		},
		{
			Name:  "INFINITY_PORT",
			Value: "8000",
		},
		{
			// TODO: Conditionally set this token based on whether
			// huggingface is the model source.
			Name: "HF_TOKEN",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: r.HuggingfaceSecretName,
					},
					Key:      "token",
					Optional: ptr.To(true),
				},
			},
		},
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
			RuntimeClassName:   c.RuntimeClassName,
			ServiceAccountName: r.ModelServerPods.ModelServiceAccountName,
			SecurityContext:    r.ModelServerPods.ModelPodSecurityContext,
			Containers: []corev1.Container{
				{
					Name:  serverContainerName,
					Image: c.Image,
					Args:  args,
					Env:   env,
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
						// Give the model 20 minutes to start up.
						FailureThreshold: 600,
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

	patchServerCacheVolumes(&pod.Spec, m, c)

	return pod
}
