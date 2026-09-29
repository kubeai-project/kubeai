package modelcontroller

import (
	kubeaiv1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

const (
	draResourceClaimName    = "gpu-claim"
	draResourceClaimRequest = "gpu"
)

func applyResourceClaimsForModel(pod *corev1.Pod, model *kubeaiv1.Model) {
	if model.Spec.ResourceClaimName == "" {
		return
	}

	resourceClaimName := model.Spec.ResourceClaimName
	updated := false
	for i := range pod.Spec.ResourceClaims {
		if pod.Spec.ResourceClaims[i].Name == draResourceClaimName {
			pod.Spec.ResourceClaims[i].ResourceClaimName = ptr.To(resourceClaimName)
			pod.Spec.ResourceClaims[i].ResourceClaimTemplateName = nil
			updated = true
			break
		}
	}
	if !updated {
		pod.Spec.ResourceClaims = append(pod.Spec.ResourceClaims, corev1.PodResourceClaim{
			Name:              draResourceClaimName,
			ResourceClaimName: ptr.To(resourceClaimName),
		})
	}

	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name != serverContainerName {
			continue
		}
		containerClaims := pod.Spec.Containers[i].Resources.Claims
		containerUpdated := false
		for j := range containerClaims {
			if containerClaims[j].Name == draResourceClaimName {
				containerClaims[j].Request = draResourceClaimRequest
				containerUpdated = true
				break
			}
		}
		if !containerUpdated {
			containerClaims = append(containerClaims, corev1.ResourceClaim{
				Name:    draResourceClaimName,
				Request: draResourceClaimRequest,
			})
		}
		pod.Spec.Containers[i].Resources.Claims = containerClaims
	}
}
