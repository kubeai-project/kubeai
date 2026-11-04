package modelcontroller

import (
	"testing"

	v1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

func Test_applyResourceClaimsForModel(t *testing.T) {
	t.Parallel()

	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: serverContainerName,
				},
			},
		},
	}
	model := &v1.Model{
		Spec: v1.ModelSpec{
			ResourceClaimName: "kubeai-gpu-mps-shared",
		},
	}

	applyResourceClaimsForModel(pod, model)

	require.Len(t, pod.Spec.ResourceClaims, 1)
	require.Equal(t, draResourceClaimName, pod.Spec.ResourceClaims[0].Name)
	require.NotNil(t, pod.Spec.ResourceClaims[0].ResourceClaimName)
	require.Equal(t, model.Spec.ResourceClaimName, *pod.Spec.ResourceClaims[0].ResourceClaimName)

	require.Len(t, pod.Spec.Containers[0].Resources.Claims, 1)
	require.Equal(t, draResourceClaimName, pod.Spec.Containers[0].Resources.Claims[0].Name)
	require.Equal(t, draResourceClaimRequest, pod.Spec.Containers[0].Resources.Claims[0].Request)
}

func Test_applyResourceClaimsForModel_empty(t *testing.T) {
	t.Parallel()

	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			ResourceClaims: []corev1.PodResourceClaim{
				{
					Name:              draResourceClaimName,
					ResourceClaimName: ptr.To("existing-claim"),
				},
			},
			Containers: []corev1.Container{
				{
					Name: serverContainerName,
					Resources: corev1.ResourceRequirements{
						Claims: []corev1.ResourceClaim{
							{
								Name:    draResourceClaimName,
								Request: "existing",
							},
						},
					},
				},
			},
		},
	}
	model := &v1.Model{}

	applyResourceClaimsForModel(pod, model)

	require.Len(t, pod.Spec.ResourceClaims, 1)
	require.Equal(t, "existing-claim", *pod.Spec.ResourceClaims[0].ResourceClaimName)
	require.Len(t, pod.Spec.Containers[0].Resources.Claims, 1)
	require.Equal(t, "existing", pod.Spec.Containers[0].Resources.Claims[0].Request)
}
