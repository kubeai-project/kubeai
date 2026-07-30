package modelcontroller

import (
	"github.com/kubeai-project/kubeai/internal/config"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

// applyResourceClaims patches pod to wire DRA claims based on the resource profile config.
// It is a no-op when dra is nil (device plugin profile, unchanged behaviour).
func applyResourceClaims(pod *corev1.Pod, dra *config.DRAConfig) {
	if dra == nil {
		return
	}

	podClaim := corev1.PodResourceClaim{Name: dra.ClaimName}
	switch {
	case dra.ResourceClaimTemplateName != "":
		podClaim.ResourceClaimTemplateName = ptr.To(dra.ResourceClaimTemplateName)
	case dra.ResourceClaimName != "":
		podClaim.ResourceClaimName = ptr.To(dra.ResourceClaimName)
	}

	// Upsert into pod.Spec.ResourceClaims.
	updated := false
	for i := range pod.Spec.ResourceClaims {
		if pod.Spec.ResourceClaims[i].Name == dra.ClaimName {
			pod.Spec.ResourceClaims[i] = podClaim
			updated = true
			break
		}
	}
	if !updated {
		pod.Spec.ResourceClaims = append(pod.Spec.ResourceClaims, podClaim)
	}

	// Upsert container claim reference on the server container only.
	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name != serverContainerName {
			continue
		}
		claims := pod.Spec.Containers[i].Resources.Claims
		claimUpdated := false
		for j := range claims {
			if claims[j].Name == dra.ClaimName {
				claims[j].Request = dra.ClaimRequest
				claimUpdated = true
				break
			}
		}
		if !claimUpdated {
			claims = append(claims, corev1.ResourceClaim{
				Name:    dra.ClaimName,
				Request: dra.ClaimRequest,
			})
		}
		pod.Spec.Containers[i].Resources.Claims = claims
	}
}
