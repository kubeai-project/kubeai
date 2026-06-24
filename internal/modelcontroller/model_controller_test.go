package modelcontroller

import (
	"encoding/json"
	"testing"

	"context"
	v1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/kubeai-project/kubeai/internal/config"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
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

func TestReconcileHeadlessService(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = v1.AddToScheme(scheme)

	const testModelUID = "12345678-1234-1234-1234-123456789012"
	model := &v1.Model{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-model",
			Namespace: "default",
			UID:       types.UID(testModelUID),
		},
		Spec: v1.ModelSpec{
			Engine: v1.VLLMEngine,
		},
	}
	svcName := "test-model-123456"

	t.Run("external_creates_service", func(t *testing.T) {
		cl := fake.NewClientBuilder().WithScheme(scheme).Build()
		r := ModelReconciler{
			Client:    cl,
			Scheme:    scheme,
			ProxyMode: config.ProxyModeExternal,
		}

		err := r.reconcileHeadlessService(context.Background(), model, ModelConfig{})
		require.NoError(t, err)

		var svc corev1.Service
		err = cl.Get(context.Background(), types.NamespacedName{Name: svcName, Namespace: "default"}, &svc)
		require.NoError(t, err)

		require.Equal(t, corev1.ClusterIPNone, svc.Spec.ClusterIP)
		require.Equal(t, "test-model", svc.Spec.Selector["kubeai.org/model"])
		require.Equal(t, testModelUID, svc.Spec.Selector["kubeai.org/model-uid"])
		require.Equal(t, int32(8000), svc.Spec.Ports[0].Port)
		require.Equal(t, intstr.FromInt32(8000), svc.Spec.Ports[0].TargetPort)
		require.Equal(t, 1, len(svc.OwnerReferences))
		require.Equal(t, "test-model", svc.OwnerReferences[0].Name)
	})

	t.Run("internal_deletes_service", func(t *testing.T) {
		existingSvc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcName,
				Namespace: "default",
			},
			Spec: corev1.ServiceSpec{
				ClusterIP: corev1.ClusterIPNone,
			},
		}
		cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(existingSvc).Build()
		r := ModelReconciler{
			Client:    cl,
			Scheme:    scheme,
			ProxyMode: config.ProxyModeInternal,
		}

		err := r.reconcileHeadlessService(context.Background(), model, ModelConfig{})
		require.NoError(t, err)

		var svc corev1.Service
		err = cl.Get(context.Background(), types.NamespacedName{Name: svcName, Namespace: "default"}, &svc)
		require.Error(t, err) // Should be not found
	})
}
