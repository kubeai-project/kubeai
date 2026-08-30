package modelcontroller

import (
	"fmt"
	"sort"
	"strings"

	kubeaiv1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// llamaCppCacheDir is where llama-server stores models it downloads itself via
// "-hf". It is backed by an emptyDir so the download does not depend on $HOME
// being writable under a restrictive Pod security context.
const llamaCppCacheDir = "/llama-cache"

func (r *ModelReconciler) llamaCppPodForModel(m *kubeaiv1.Model, c ModelConfig) *corev1.Pod {
	lbs := labelsForModel(m)
	ann := r.annotationsForModel(m)
	if _, ok := ann[kubeaiv1.ModelPodPortAnnotation]; !ok {
		ann[kubeaiv1.ModelPodPortAnnotation] = "8000"
	}

	// llama-server parses args left to right and the last occurrence wins, so
	// .spec.args is appended last and can override anything set here.
	args := []string{
		"--host", "0.0.0.0",
		"--port", "8000",
		// Serve the Model name on /v1/models and accept it in the "model"
		// field of OpenAI requests.
		"--alias", m.Name,
		// vLLM exposes /metrics unconditionally; match that so a PodMonitor
		// works without per-model args.
		"--metrics",
	}
	args = append(args, llamaCppModelArgs(c.Source.url)...)
	args = append(args, llamaCppFeatureArgs(m.Spec.Features)...)
	args = append(args, m.Spec.Args...)

	var (
		env          []corev1.EnvVar
		volumes      []corev1.Volume
		volumeMounts []corev1.VolumeMount
	)
	if c.Source.url.scheme == "hf" {
		env = append(env, corev1.EnvVar{Name: "LLAMA_CACHE", Value: llamaCppCacheDir})
		volumes = append(volumes, corev1.Volume{
			Name:         "llama-cache",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "llama-cache",
			MountPath: llamaCppCacheDir,
		})
	}

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
					Name: serverContainerName,
					// No Command: the llama.cpp "server" images set
					// ENTRYPOINT ["/app/llama-server"].
					Image:           c.Image,
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
						// /health returns 503 while the model is loading.
						// Give the model 3 hours to start up.
						FailureThreshold: 5400,
						PeriodSeconds:    2,
						TimeoutSeconds:   2,
						SuccessThreshold: 1,
						ProbeHandler:     llamaCppHealthProbe(),
					},
					ReadinessProbe: &corev1.Probe{
						FailureThreshold: 3,
						PeriodSeconds:    10,
						TimeoutSeconds:   2,
						SuccessThreshold: 1,
						ProbeHandler:     llamaCppHealthProbe(),
					},
					LivenessProbe: &corev1.Probe{
						FailureThreshold: 3,
						PeriodSeconds:    30,
						TimeoutSeconds:   3,
						SuccessThreshold: 1,
						ProbeHandler:     llamaCppHealthProbe(),
					},
					VolumeMounts: volumeMounts,
				},
			},
			Volumes: volumes,
		},
	}

	patchFileVolumes(&pod.Spec, m)
	patchServerCacheVolumes(&pod.Spec, m, c)
	c.Source.modelSourcePodAdditions.applyToPodSpec(&pod.Spec, 0)

	return pod
}

func llamaCppHealthProbe() corev1.ProbeHandler {
	return corev1.ProbeHandler{
		HTTPGet: &corev1.HTTPGetAction{
			Path: "/health",
			Port: intstr.FromString("http"),
		},
	}
}

// llamaCppModelArgs resolves the flag that points llama-server at the model.
//
//	hf://<repo>[:<quant>]        -> -hf <repo>[:<quant>], downloaded by llama-server
//	pvc://<pvc>/<dir>?model=<f>  -> -m /model/<f>
//	oci://<ref>?model=<f>        -> -m /model/<f>
//	pvc://<pvc>/<file>.gguf      -> -m /model
//
// The last form relies on kubelet bind-mounting a single file at /model. It
// only works for unsharded models: llama.cpp derives the paths of sibling
// shards from the file name, which the mount does not preserve.
func llamaCppModelArgs(u modelURL) []string {
	if u.scheme == "hf" {
		return []string{"-hf", u.ref}
	}
	if u.modelParam != "" {
		return []string{"-m", "/model/" + u.modelParam}
	}
	return []string{"-m", "/model"}
}

// llamaCppFeatureArgs maps Model features onto the flags that enable the
// matching routes. /v1/embeddings and /v1/rerank report "This server does not
// support embeddings" unless one of these is set.
func llamaCppFeatureArgs(features []kubeaiv1.ModelFeature) []string {
	var embedding, reranking bool
	for _, f := range features {
		switch f {
		case kubeaiv1.ModelFeatureTextEmbedding:
			embedding = true
		case kubeaiv1.ModelFeatureReranking:
			reranking = true
		}
	}
	switch {
	case reranking:
		// --reranking implies --embedding and forces the "rank" pooling type.
		return []string{"--reranking"}
	case embedding:
		return []string{"--embedding"}
	}
	return nil
}

// validateLlamaCppModel rejects Model specs the LlamaCpp engine cannot serve.
// The URL scheme and cacheProfile are covered by CRD validation.
func validateLlamaCppModel(m *kubeaiv1.Model) error {
	u, err := parseModelURL(m.Spec.URL)
	if err != nil {
		return err
	}
	if p := u.modelParam; p != "" {
		if strings.HasPrefix(p, "/") || strings.Contains(p, "..") {
			return fmt.Errorf("invalid %q parameter %q: must be a relative path without %q", "model", p, "..")
		}
	} else {
		// llama-server's -m flag takes a file. An oci:// source is always
		// mounted as a directory, and a pvc:// source only resolves to a file
		// when its subpath names one.
		switch u.scheme {
		case "oci":
			return fmt.Errorf("the LlamaCpp engine requires a %q query parameter naming the GGUF file for %q urls", "model", "oci://")
		case "pvc":
			if !strings.HasSuffix(u.path, ".gguf") {
				return fmt.Errorf("the LlamaCpp engine requires either a %q query parameter or a url ending in %q, got %q", "model", ".gguf", m.Spec.URL)
			}
		}
	}

	var generation, embedding bool
	for _, f := range m.Spec.Features {
		switch f {
		case kubeaiv1.ModelFeatureSpeechToText:
			return fmt.Errorf("the LlamaCpp engine does not support the %q feature", f)
		case kubeaiv1.ModelFeatureTextGeneration:
			generation = true
		case kubeaiv1.ModelFeatureTextEmbedding, kubeaiv1.ModelFeatureReranking:
			embedding = true
		}
	}
	if generation && embedding {
		return fmt.Errorf("the LlamaCpp engine cannot serve %q together with %q or %q: the embedding flags restrict llama-server to embeddings",
			kubeaiv1.ModelFeatureTextGeneration, kubeaiv1.ModelFeatureTextEmbedding, kubeaiv1.ModelFeatureReranking)
	}
	return nil
}
