package modelcontroller

import (
	"testing"

	"github.com/kubeai-project/kubeai/internal/config"
	"github.com/kubeai-project/kubeai/internal/k8sutils"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func Test_applyJSONPatchToPod(t *testing.T) {
	t.Parallel()

	emptyPod := &corev1.Pod{}
	emptyPodHash := k8sutils.PodHash(emptyPod.Spec)

	cases := map[string]struct {
		modelPod     *corev1.Pod
		patches      []config.JSONPatch
		want         *corev1.Pod
		errorString  *string
		expectedHash *string
	}{
		"no-patch": {
			modelPod:     emptyPod,
			patches:      nil,
			want:         emptyPod,
			expectedHash: ptr.To(emptyPodHash), // default hash
		},
		"error-patch": {
			modelPod: emptyPod,
			patches: []config.JSONPatch{
				{Op: "invalid", Path: "/spec/containers/0/image", Value: "new-image"},
			},
			want:         emptyPod,
			expectedHash: ptr.To(emptyPodHash), // since the patch failed it should not change the hash
			errorString:  ptr.To("apply pod patch: Unexpected kind: invalid"),
		},
		"replace-image": {
			modelPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "test-image",
						},
					},
				},
			},
			patches: []config.JSONPatch{
				{Op: "replace", Path: "/spec/containers/0/image", Value: "new-image"},
			},
			want: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "test-container",
							Image: "new-image",
						},
					},
				},
			},
		},
		"add-preemption-policy": {
			modelPod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
				Spec: corev1.PodSpec{},
			},
			patches: []config.JSONPatch{
				{Op: "add", Path: "/spec/preemptionPolicy", Value: "Never"},
			},
			want: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "test-namespace",
				},
				Spec: corev1.PodSpec{
					PreemptionPolicy: ptr.To(corev1.PreemptNever),
				},
			},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			gotHash := k8sutils.PodHash(c.modelPod.Spec)

			err := applyJSONPatchToPod(c.patches, c.modelPod)
			if c.errorString != nil {
				require.ErrorContains(t, err, *c.errorString)
			} else {
				require.NoError(t, err)
			}

			if c.expectedHash != nil {
				require.Equal(t, *c.expectedHash, gotHash, "expected pod hash to be %s", *c.expectedHash)
			}

			require.Equal(t, c.want, c.modelPod, "expected pod to be patched correctly")
		})
	}
}
