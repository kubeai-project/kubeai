package modelcontroller

import (
	"testing"

	kubeaiv1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/kubeai-project/kubeai/internal/config"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func Test_infinityPodForModel_appliesSecurityContexts(t *testing.T) {
	t.Parallel()

	podSecurityContext := &corev1.PodSecurityContext{RunAsNonRoot: ptr.To(true)}
	containerSecurityContext := &corev1.SecurityContext{AllowPrivilegeEscalation: ptr.To(false)}
	r := &ModelReconciler{
		ModelServerPods: config.ModelServerPods{
			ModelPodSecurityContext:       podSecurityContext,
			ModelContainerSecurityContext: containerSecurityContext,
		},
	}
	const modelURL = "hf://sentence-transformers/all-MiniLM-L6-v2"
	source, err := r.parseModelSource(modelURL)
	require.NoError(t, err)
	m := &kubeaiv1.Model{
		ObjectMeta: metav1.ObjectMeta{Name: "model-name", Namespace: "default"},
		Spec: kubeaiv1.ModelSpec{
			URL:    modelURL,
			Engine: kubeaiv1.InfinityEngine,
		},
	}

	pod := r.infinityPodForModel(m, ModelConfig{Source: source})

	require.Equal(t, podSecurityContext, pod.Spec.SecurityContext)
	require.Len(t, pod.Spec.Containers, 1)
	require.Equal(t, containerSecurityContext, pod.Spec.Containers[0].SecurityContext)
}
