package modelcontroller

import (
	"encoding/json"
	"testing"

	v1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/kubeai-project/kubeai/internal/config"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func Test_getModelConfig(t *testing.T) {
	r := ModelReconciler{
		ResourceProfiles: map[string]config.ResourceProfile{
			"none": {},
			"my-gpu": {
				Limits: corev1.ResourceList{
					"nvidia.com/gpu": resource.MustParse("1"),
				},
				Requests: corev1.ResourceList{
					"memory": resource.MustParse("24Gi"),
				},
				NodeSelector: map[string]string{
					"my-gpu": "true",
				},
				Affinity: &corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key:      "my-gpu-key",
											Operator: corev1.NodeSelectorOpIn,
											Values:   []string{"my-gpu-val"},
										},
									},
								},
							},
						},
					},
				},
				Tolerations: []corev1.Toleration{
					{
						Key:      "my-gpu-toleration",
						Operator: corev1.TolerationOpExists,
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
			},
			"tolerations-only": {
				Tolerations: []corev1.Toleration{
					{
						Key:      "toleration1",
						Operator: corev1.TolerationOpExists,
						Effect:   corev1.TaintEffectNoSchedule,
					},
					{
						Key:      "toleration2",
						Operator: corev1.TolerationOpExists,
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
			},
		},
		ModelServers: config.ModelServers{
			VLLM: config.ModelServer{
				Images: map[string]string{
					"default": "default-vllm-image",
				},
			},
		},
	}

	cases := []struct {
		name     string
		input    *v1.Model
		expected ModelConfig
	}{
		{
			name: "basic",
			input: &v1.Model{
				Spec: v1.ModelSpec{
					Engine:          v1.VLLMEngine,
					ResourceProfile: "my-gpu:2",
					URL:             "hf://some-repo/some-model",
				},
			},
			expected: ModelConfig{
				Image: "default-vllm-image",
				ResourceProfile: config.ResourceProfile{
					Limits: corev1.ResourceList{
						"nvidia.com/gpu": resource.MustParse("2"),
					},
					Requests: corev1.ResourceList{
						"memory": resource.MustParse("48Gi"),
					},
					NodeSelector: map[string]string{
						"my-gpu": "true",
					},
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      "my-gpu-key",
												Operator: corev1.NodeSelectorOpIn,
												Values:   []string{"my-gpu-val"},
											},
										},
									},
								},
							},
						},
					},
					Tolerations: []corev1.Toleration{
						{
							Key:      "my-gpu-toleration",
							Operator: corev1.TolerationOpExists,
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			model := c.input
			config, err := r.getModelConfig(model)
			require.NoError(t, err)
			requireEqualJSON(t, c.expected, config)
		})
	}
}

func requireEqualJSON(t *testing.T, a, b interface{}) {
	jsonA, err := json.Marshal(a)
	require.NoError(t, err)
	jsonB, err := json.Marshal(b)
	require.NoError(t, err)
	require.JSONEq(t, string(jsonA), string(jsonB))
}
