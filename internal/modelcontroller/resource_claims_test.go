package modelcontroller

import (
	"testing"

	"github.com/kubeai-project/kubeai/internal/config"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

func podWithServerContainer() *corev1.Pod {
	return &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{Name: serverContainerName},
			},
		},
	}
}

func Test_applyResourceClaims_nil(t *testing.T) {
	t.Parallel()
	pod := podWithServerContainer()
	applyResourceClaims(pod, nil)
	require.Empty(t, pod.Spec.ResourceClaims)
	require.Empty(t, pod.Spec.Containers[0].Resources.Claims)
}

func Test_applyResourceClaims_template(t *testing.T) {
	t.Parallel()
	pod := podWithServerContainer()
	dra := &config.DRAConfig{
		ResourceClaimTemplateName: "nvidia-h100-exclusive",
		ClaimName:                 "gpu-claim",
		ClaimRequest:              "gpu",
	}

	applyResourceClaims(pod, dra)

	require.Len(t, pod.Spec.ResourceClaims, 1)
	require.Equal(t, "gpu-claim", pod.Spec.ResourceClaims[0].Name)
	require.Equal(t, "nvidia-h100-exclusive", *pod.Spec.ResourceClaims[0].ResourceClaimTemplateName)
	require.Nil(t, pod.Spec.ResourceClaims[0].ResourceClaimName)

	require.Len(t, pod.Spec.Containers[0].Resources.Claims, 1)
	require.Equal(t, "gpu-claim", pod.Spec.Containers[0].Resources.Claims[0].Name)
	require.Equal(t, "gpu", pod.Spec.Containers[0].Resources.Claims[0].Request)
}

func Test_applyResourceClaims_shared(t *testing.T) {
	t.Parallel()
	pod := podWithServerContainer()
	dra := &config.DRAConfig{
		ResourceClaimName: "nvidia-mps-shared",
		ClaimName:         "gpu-claim",
		ClaimRequest:      "gpu",
	}

	applyResourceClaims(pod, dra)

	require.Len(t, pod.Spec.ResourceClaims, 1)
	require.Equal(t, "gpu-claim", pod.Spec.ResourceClaims[0].Name)
	require.Equal(t, "nvidia-mps-shared", *pod.Spec.ResourceClaims[0].ResourceClaimName)
	require.Nil(t, pod.Spec.ResourceClaims[0].ResourceClaimTemplateName)

	require.Len(t, pod.Spec.Containers[0].Resources.Claims, 1)
	require.Equal(t, "gpu-claim", pod.Spec.Containers[0].Resources.Claims[0].Name)
	require.Equal(t, "gpu", pod.Spec.Containers[0].Resources.Claims[0].Request)
}

func Test_applyResourceClaims_idempotent(t *testing.T) {
	t.Parallel()
	// Pre-existing claims should be overwritten, not duplicated.
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			ResourceClaims: []corev1.PodResourceClaim{
				{
					Name:              "gpu-claim",
					ResourceClaimName: ptr.To("old-claim"),
				},
			},
			Containers: []corev1.Container{
				{
					Name: serverContainerName,
					Resources: corev1.ResourceRequirements{
						Claims: []corev1.ResourceClaim{
							{Name: "gpu-claim", Request: "old"},
						},
					},
				},
			},
		},
	}
	dra := &config.DRAConfig{
		ResourceClaimTemplateName: "new-template",
		ClaimName:                 "gpu-claim",
		ClaimRequest:              "gpu",
	}

	applyResourceClaims(pod, dra)

	require.Len(t, pod.Spec.ResourceClaims, 1)
	require.Equal(t, "new-template", *pod.Spec.ResourceClaims[0].ResourceClaimTemplateName)
	require.Nil(t, pod.Spec.ResourceClaims[0].ResourceClaimName)

	require.Len(t, pod.Spec.Containers[0].Resources.Claims, 1)
	require.Equal(t, "gpu", pod.Spec.Containers[0].Resources.Claims[0].Request)
}
