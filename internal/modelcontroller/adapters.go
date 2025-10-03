package modelcontroller

import (
	"context"
	"fmt"
	"sort"
	"strings"

	v1 "github.com/substratusai/kubeai/api/k8s/v1"
	"github.com/substratusai/kubeai/internal/k8sutils"
	"github.com/substratusai/kubeai/internal/vllmclient"
	corev1 "k8s.io/api/core/v1"
)

const (
	loaderContainerName = "loader"
)

// reconcileAdapters ensures that the specified adapters are loaded in the model server pods.
// Loaded adapters are identified by the presence of a Pod label with the adapter name and the hash
// of the adapter URL.
// At request-time, the endpoint resolver will inspect these labels to determine which adapters
// are loaded in the pod.
func (r *ModelReconciler) reconcileAdapters(ctx context.Context, pods []*corev1.Pod, adapters []v1.Adapter) error {
	type reconcileParam struct {
		pod         *corev1.Pod
		toEnsure    []v1.Adapter
		toRemoveIDs []string
		engine      string
	}
	var reconcileList []reconcileParam

	for _, pod := range pods {
		param := reconcileParam{
			pod: pod,
		}

		if pod.Labels == nil {
			continue
		}
		switch pod.Labels[appKubernetesIOName] {
		case strings.ToLower(v1.VLLMEngine):
			param.engine = v1.VLLMEngine
		default:
			continue
		}

		deletionCandidates := getLabelledAdapters(pod)

		for _, adapter := range adapters {
			if k8sutils.GetLabel(pod, v1.PodAdapterLabel(adapter.Name)) != k8sutils.StringHash(adapter.URL) {
				param.toEnsure = append(param.toEnsure, adapter)
			} else {
				// Matches, so don't delete.
				delete(deletionCandidates, adapter.Name)
			}
		}

		for adapterID := range deletionCandidates {
			param.toRemoveIDs = append(param.toRemoveIDs, adapterID)
		}

		reconcileList = append(reconcileList, param)
	}

	for _, param := range reconcileList {
		// TODO: Parallelize
		addr := getPodModelServerAddr(param.pod)
		if !k8sutils.ContainerIsReady(param.pod, loaderContainerName) {
			return errReturnEarly
		}
		for _, adapter := range param.toEnsure {
			if err := r.execAdapterLoad(ctx, param.pod, adapter); err != nil {
				return fmt.Errorf("exec adapter load for pod %q: %w", param.pod.Namespace+"/"+param.pod.Name, err)
			}
			switch param.engine {
			case v1.VLLMEngine:
				if err := r.VLLMClient.LoadLoraAdapter(ctx, addr, vllmclient.LoadAdapterRequest{
					LoraName: adapter.Name,
					LoraPath: adapterDir(adapter),
					Options: vllmclient.LoadAdapterRequestOptions{
						// It is possible that the adapter is already loaded, but updating the Pod labels
						// failed. In this case, we ignore the error and continue.
						IgnoreAlreadyLoaded: true,
					},
				}); err != nil {
					return fmt.Errorf("load vllm adapter %q: %w", adapter.Name, err)
				}
			}
			if err := r.updatePodAddLabel(ctx, param.pod, v1.PodAdapterLabel(adapter.Name), k8sutils.StringHash(adapter.URL)); err != nil {
				return fmt.Errorf("update pod labels for pod %q: %w", param.pod.Namespace+"/"+param.pod.Name, err)
			}
		}
		for _, adapterID := range param.toRemoveIDs {
			if err := r.execAdapterUnload(ctx, param.pod, adapterID); err != nil {
				return fmt.Errorf("exec adapter unload for pod %q: %w", param.pod.Namespace+"/"+param.pod.Name, err)
			}
			switch param.engine {
			case v1.VLLMEngine:
				if err := r.VLLMClient.UnloadLoraAdapter(ctx, addr, vllmclient.UnloadAdapterRequest{
					LoraName: adapterID,
					Options: vllmclient.UnloadAdapterRequestOptions{
						// It is possible that the adapter is already unloaded, but updating the Pod labels
						// failed. In this case, we ignore the error and continue.
						IgnoreNotFound: true,
					},
				}); err != nil {
					return fmt.Errorf("unload vllm adapter %q: %w", adapterID, err)
				}
			}
			if err := r.updatePodRemoveLabel(ctx, param.pod, v1.PodAdapterLabel(adapterID)); err != nil {
				return fmt.Errorf("update pod labels for pod %q: %w", param.pod.Namespace+"/"+param.pod.Name, err)
			}
		}
	}

	return nil
}

func getPodModelServerAddr(pod *corev1.Pod) string {
	// Example:
	//
	// kind: Pod
	// annotations:
	//   model-pod-ip: "127.0.0.1"
	//   model-pod-port: "7000"
	//
	var ip, port string
	if pod.Annotations != nil {
		if v, ok := pod.Annotations[v1.ModelPodIPAnnotation]; ok {
			ip = v
		}
		if v, ok := pod.Annotations[v1.ModelPodPortAnnotation]; ok {
			port = v
		}
	}
	if ip == "" {
		ip = pod.Status.PodIP
	}
	return fmt.Sprintf("http://%s:%s", ip, port)
}

// execAdapterLoad executes the adapter load command in the adapter loader container.
func (r *ModelReconciler) execAdapterLoad(ctx context.Context, pod *corev1.Pod, adapter v1.Adapter) error {
	if err := r.execPod(ctx, pod, loaderContainerName, []string{
		"load", adapter.URL, adapterDir(adapter),
	}); err != nil {
		return fmt.Errorf("exec adapter load: %w", err)
	}
	return nil
}

func (r *ModelReconciler) execAdapterUnload(ctx context.Context, pod *corev1.Pod, adapterID string) error {
	if err := r.execPod(ctx, pod, loaderContainerName, []string{
		"rm", "-rf", adapterDir(v1.Adapter{Name: adapterID}),
	}); err != nil {
		return fmt.Errorf("exec adapter load: %w", err)
	}
	return nil
}

const (
	adaptersVolName = "adapters"
	adaptersRootDir = "/adapters"
)

func adapterDir(a v1.Adapter) string {
	return fmt.Sprintf("%s/%s", adaptersRootDir, a.Name)
}

func (r *ModelReconciler) patchServerAdapterLoader(podSpec *corev1.PodSpec, m *v1.Model, image string) {
	if m.Spec.Adapters == nil {
		return
	}
	var env []corev1.EnvVar
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
	podSpec.Volumes = append(podSpec.Volumes, corev1.Volume{
		Name: adaptersVolName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	})
	for i := range podSpec.Containers {
		if podSpec.Containers[i].Name == serverContainerName {
			podSpec.Containers[i].VolumeMounts = append(podSpec.Containers[i].VolumeMounts, corev1.VolumeMount{
				Name:      adaptersVolName,
				MountPath: adaptersRootDir,
				ReadOnly:  true,
			})
		}
	}

	loaderContainer := corev1.Container{
		Name:            loaderContainerName,
		Image:           image,
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env:             env,
		SecurityContext: r.ModelServerPods.ModelContainerSecurityContext,
		Command:         []string{"sleep", "infinity"},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      adaptersVolName,
				MountPath: adaptersRootDir,
			},
		},
	}
	podSpec.Containers = append(podSpec.Containers, loaderContainer)
	r.modelAuthCredentialsForAllSources().applyToPodSpec(podSpec, len(podSpec.Containers)-1)
	r.modelEnvFrom(m).applyToPodSpec(podSpec, len(podSpec.Containers)-1)
}

func getLabelledAdapters(pod *corev1.Pod) map[string]struct{} {
	ids := make(map[string]struct{})
	for k := range pod.Labels {
		if strings.HasPrefix(k, v1.PodAdapterLabelPrefix) {
			ids[strings.TrimPrefix(k, v1.PodAdapterLabelPrefix)] = struct{}{}
		}
	}
	return ids
}
