package modelcontroller

import (
	"context"
	"testing"

	kubeaiv1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/kubeai-project/kubeai/internal/config"
	"github.com/kubeai-project/kubeai/internal/k8sutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	apitypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	lwsv1 "sigs.k8s.io/lws/api/leaderworkerset/v1"
)

func testLWSReconciler(t *testing.T) *ModelReconciler {
	t.Helper()
	return &ModelReconciler{
		DistributedInference: true,
		ModelRollouts:        config.ModelRollouts{Surge: 1},
		ModelServers:         config.ModelServers{VLLM: config.ModelServer{Images: map[string]string{"default": "vllm/vllm:test"}}},
	}
}

func testLWSModel(t *testing.T) *kubeaiv1.Model {
	t.Helper()
	return &kubeaiv1.Model{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-model",
			Namespace: "default",
		},
		Spec: kubeaiv1.ModelSpec{
			Engine:                kubeaiv1.VLLMEngine,
			URL:                   "hf://test/model",
			Replicas:              ptr.To[int32](1),
			TargetRequests:        ptr.To[int32](100),
			MinReplicas:           0,
			ScaleDownDelaySeconds: ptr.To[int64](30),
		},
	}
}

func testLWSModelConfig(t *testing.T, r *ModelReconciler) ModelConfig {
	t.Helper()
	src, err := r.parseModelSource("hf://test/model")
	require.NoError(t, err)
	return ModelConfig{
		ResourceProfile: config.ResourceProfile{
			Requests: corev1.ResourceList{
				"nvidia.com/gpu": resource.MustParse("1"),
			},
			Limits: corev1.ResourceList{
				"nvidia.com/gpu": resource.MustParse("1"),
			},
		},
		Source: src,
		Image:  "vllm/vllm:test",
		LWSConfig: &LWSConfig{
			TensorParallelSize:   2,
			PipelineParallelSize: 3,
		},
		PodBuilder: r.vLLMPodForModel,
	}
}

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(s))
	require.NoError(t, lwsv1.AddToScheme(s))
	require.NoError(t, kubeaiv1.AddToScheme(s))
	return s
}

func TestCalculateLWSPlan(t *testing.T) {
	specs := map[string]struct {
		existingLWS       *lwsv1.LeaderWorkerSet
		replicas          int32
		ppSize            int
		expectCreate      bool
		expectScale       bool
		expectUpgrade     bool
		expectErr         bool
		expectStatusAll   int32
		expectStatusReady int32
	}{
		"Creates LWS when none exists": {
			replicas:          1,
			ppSize:            2,
			expectCreate:      true,
			expectStatusAll:   0,
			expectStatusReady: 0,
		},
		"No-op when LWS at correct scale": {
			existingLWS: &lwsv1.LeaderWorkerSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test-model", Namespace: "default"},
				Spec:       lwsv1.LeaderWorkerSetSpec{Replicas: ptr.To[int32](2)},
				Status:     lwsv1.LeaderWorkerSetStatus{Replicas: 2, ReadyReplicas: 2},
			},
			replicas:          2,
			ppSize:            2,
			expectStatusAll:   2,
			expectStatusReady: 2,
		},
		"Scales up when desired > observed": {
			existingLWS: &lwsv1.LeaderWorkerSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test-model", Namespace: "default"},
				Spec:       lwsv1.LeaderWorkerSetSpec{Replicas: ptr.To[int32](1)},
				Status:     lwsv1.LeaderWorkerSetStatus{Replicas: 1, ReadyReplicas: 1},
			},
			replicas:          3,
			ppSize:            2,
			expectScale:       true,
			expectStatusAll:   1,
			expectStatusReady: 1,
		},
		"Scales down when desired < observed": {
			existingLWS: &lwsv1.LeaderWorkerSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test-model", Namespace: "default"},
				Spec:       lwsv1.LeaderWorkerSetSpec{Replicas: ptr.To[int32](3)},
				Status:     lwsv1.LeaderWorkerSetStatus{Replicas: 3, ReadyReplicas: 3},
			},
			replicas:          1,
			ppSize:            2,
			expectScale:       true,
			expectStatusAll:   3,
			expectStatusReady: 3,
		},
		"Rejects group size < 2": {
			replicas:  1,
			ppSize:    1,
			expectErr: true,
		},
		"Update LWS when model spec changes": {
			existingLWS: &lwsv1.LeaderWorkerSet{
				ObjectMeta: metav1.ObjectMeta{Name: "test-model", Namespace: "default"},
				Spec: lwsv1.LeaderWorkerSetSpec{
					Replicas: ptr.To[int32](2),
					LeaderWorkerTemplate: lwsv1.LeaderWorkerTemplate{
						LeaderTemplate: &corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Name:  "model-server",
									Image: "vllm/vllm:test",
								}},
							},
						},
						WorkerTemplate: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{
								Containers: []corev1.Container{{
									Name:  "model-server",
									Image: "vllm/vllm:test",
								}},
							},
						},
					}},
				Status: lwsv1.LeaderWorkerSetStatus{Replicas: 2, ReadyReplicas: 2},
			},
			replicas:          2,
			ppSize:            2,
			expectStatusAll:   2,
			expectStatusReady: 2,
			expectUpgrade:     true,
		},
	}

	for name, tt := range specs {
		t.Run(name, func(t *testing.T) {
			r := testLWSReconciler(t)
			scheme := testScheme(t)
			r.Scheme = scheme

			model := testLWSModel(t)
			model.Spec.Replicas = ptr.To(tt.replicas)

			cfg := testLWSModelConfig(t, r)
			cfg.LWSConfig.PipelineParallelSize = tt.ppSize

			if tt.existingLWS != nil {
				desiredLWS, err := r.buildLeaderWorkerSet(model, cfg)
				require.NoError(t, err)

				leaderExpededHash := k8sutils.PodHash(desiredLWS.Spec.LeaderWorkerTemplate.LeaderTemplate.Spec)
				workerExpededHash := k8sutils.PodHash(desiredLWS.Spec.LeaderWorkerTemplate.WorkerTemplate.Spec)

				if tt.existingLWS.Labels == nil {
					tt.existingLWS.Labels = map[string]string{}
				}

				if _, ok := tt.existingLWS.Labels[kubeaiv1.LeaderHashLabel]; !ok {
					if tt.expectUpgrade {
						tt.existingLWS.Labels[kubeaiv1.LeaderHashLabel] = "stale-leader-hash"
					} else {
						tt.existingLWS.Labels[kubeaiv1.LeaderHashLabel] = leaderExpededHash
					}
				}

				if _, ok := tt.existingLWS.Labels[kubeaiv1.WorkerHashLabel]; !ok {
					if tt.expectUpgrade {
						tt.existingLWS.Labels[kubeaiv1.WorkerHashLabel] = "stale-worker-hash"
					} else {
						tt.existingLWS.Labels[kubeaiv1.WorkerHashLabel] = workerExpededHash
					}
				}
			}

			var objs []client.Object
			if tt.existingLWS != nil {
				objs = append(objs, tt.existingLWS)
			}
			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objs...).
				Build()
			r.Client = fakeClient

			plan, err := r.calculateLWSPlan(context.Background(), model, cfg)

			if tt.expectErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			lp := plan

			if tt.expectCreate {
				assert.NotNil(t, lp.toCreate, "expected LWS creation")
			} else {
				assert.Nil(t, lp.toCreate, "unexpected LWS creation")
			}

			if tt.expectScale {
				assert.NotNil(t, lp.toScale, "expected LWS scaling")
			} else {
				assert.Nil(t, lp.toScale, "unexpected LWS scaling")
			}

			if tt.expectUpgrade {
				assert.NotNil(t, lp.toUpgrade, "expected LWS upgrade")
			} else {
				assert.Nil(t, lp.toUpgrade, "unexpected LWS upgrade")
			}

			assert.Equal(t, tt.expectStatusAll, model.Status.Replicas.All)
			assert.Equal(t, tt.expectStatusReady, model.Status.Replicas.Ready)
		})
	}
}

func TestBuildLeaderWorkerSet(t *testing.T) {
	r := testLWSReconciler(t)
	model := testLWSModel(t)
	cfg := testLWSModelConfig(t, r)

	lws, err := r.buildLeaderWorkerSet(model, cfg)
	require.NoError(t, err)

	specs := map[string]struct {
		check func(t *testing.T)
	}{
		"LWS name matches model": {
			check: func(t *testing.T) {
				assert.Equal(t, "test-model", lws.Name)
				assert.Equal(t, "default", lws.Namespace)
			},
		},
		"LWS has model label": {
			check: func(t *testing.T) {
				assert.Equal(t, model.Name, lws.Labels["model"])
			},
		},
		"LWS has TP/PP annotations": {
			check: func(t *testing.T) {
				assert.Equal(t, "2", lws.Annotations["kubeai.org/tensor-parallel-size"])
				assert.Equal(t, "3", lws.Annotations["kubeai.org/pipeline-parallel-size"])
			},
		},
		"Group size set correctly": {
			check: func(t *testing.T) {
				require.NotNil(t, lws.Spec.LeaderWorkerTemplate.Size)
				assert.Equal(t, int32(3), *lws.Spec.LeaderWorkerTemplate.Size)
			},
		},
		"Head pod has model label": {
			check: func(t *testing.T) {
				headLabels := lws.Spec.LeaderWorkerTemplate.LeaderTemplate.Labels
				assert.Equal(t, model.Name, headLabels["model"])
				assert.Equal(t, GroupRoleHead, headLabels[LabelGroupRole])
			},
		},
		"Head pod has Ray port": {
			check: func(t *testing.T) {
				headPorts := lws.Spec.LeaderWorkerTemplate.LeaderTemplate.Spec.Containers[0].Ports
				var found bool
				for _, p := range headPorts {
					if p.Name == rayPortName && p.ContainerPort == int32(rayPort) {
						found = true
					}
				}
				assert.True(t, found, "head pod should have Ray port")
			},
		},
		"Head pod has TP/PP args": {
			check: func(t *testing.T) {
				headArgs := lws.Spec.LeaderWorkerTemplate.LeaderTemplate.Spec.Containers[0].Args
				assert.Contains(t, headArgs, "--tensor-parallel-size=2")
				assert.Contains(t, headArgs, "--pipeline-parallel-size=3")
			},
		},
		"Worker pod has no model label": {
			check: func(t *testing.T) {
				workerLabels := lws.Spec.LeaderWorkerTemplate.WorkerTemplate.Labels
				_, hasModel := workerLabels["model"]
				assert.False(t, hasModel, "worker pod should not have 'model' label")
				assert.Equal(t, GroupRoleWorker, workerLabels[LabelGroupRole])
			},
		},
		"Worker pod runs ray start": {
			check: func(t *testing.T) {
				workerCmd := lws.Spec.LeaderWorkerTemplate.WorkerTemplate.Spec.Containers[0].Command
				require.Len(t, workerCmd, 3)
				assert.Contains(t, workerCmd[2], "worker")
				assert.Contains(t, workerCmd[2], "$(LWS_LEADER_ADDRESS)")
			},
		},
		"Worker pod has exec probes": {
			check: func(t *testing.T) {
				c := lws.Spec.LeaderWorkerTemplate.WorkerTemplate.Spec.Containers[0]
				require.NotNil(t, c.LivenessProbe)
				require.NotNil(t, c.LivenessProbe.Exec)
				assert.Contains(t, c.LivenessProbe.Exec.Command, "ray")
			},
		},
		"LWS has leader-created startup policy": {
			check: func(t *testing.T) {
				assert.Equal(t, lwsv1.LeaderCreatedStartupPolicy, lws.Spec.StartupPolicy)
			},
		},
	}

	for name, tt := range specs {
		t.Run(name, tt.check)
	}
}

func TestBuildLeaderWorkerSet_NilPodBuilder(t *testing.T) {
	r := testLWSReconciler(t)
	model := testLWSModel(t)
	cfg := testLWSModelConfig(t, r)
	cfg.PodBuilder = nil // explicitly nil

	_, err := r.buildLeaderWorkerSet(model, cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no pod builder")
}

func TestLWSPlan_Execute(t *testing.T) {
	scheme := testScheme(t)

	specs := map[string]struct {
		plan          *lwsPlan
		clientObjects []client.Object
		expectScaled  bool
		expectUpgrade bool
		expectImage   string
		expectErr     bool
	}{
		"No-op plan": {
			plan:         &lwsPlan{model: &kubeaiv1.Model{ObjectMeta: metav1.ObjectMeta{Name: "test"}}},
			expectScaled: false,
		},
		"Create LWS": {
			plan: &lwsPlan{
				model: &kubeaiv1.Model{
					ObjectMeta: metav1.ObjectMeta{Name: "m1", Namespace: "default", UID: "uid-1"},
				},
				toCreate: &lwsv1.LeaderWorkerSet{
					ObjectMeta: metav1.ObjectMeta{Name: "m1", Namespace: "default"},
					Spec:       lwsv1.LeaderWorkerSetSpec{Replicas: ptr.To[int32](1)},
				},
			},
			expectScaled: true,
		},
		"Delete existing LWS": {
			plan: &lwsPlan{
				model: &kubeaiv1.Model{ObjectMeta: metav1.ObjectMeta{Name: "m1"}},
				toDelete: &lwsv1.LeaderWorkerSet{
					ObjectMeta: metav1.ObjectMeta{Name: "lws1", Namespace: "default"},
				},
			},
			clientObjects: []client.Object{
				&lwsv1.LeaderWorkerSet{ObjectMeta: metav1.ObjectMeta{Name: "lws1", Namespace: "default"}},
			},
			expectScaled: true,
		},
		"Delete non-existent LWS is not error": {
			plan: &lwsPlan{
				model: &kubeaiv1.Model{ObjectMeta: metav1.ObjectMeta{Name: "m1"}},
				toDelete: &lwsv1.LeaderWorkerSet{
					ObjectMeta: metav1.ObjectMeta{Name: "nonexistent", Namespace: "default"},
				},
			},
			expectScaled: true,
		},
		"Upgrade existing LWS": {
			plan: &lwsPlan{
				model: &kubeaiv1.Model{
					ObjectMeta: metav1.ObjectMeta{Name: "m1", Namespace: "default", UID: "uid-1"},
				},
				toUpgrade: &lwsv1.LeaderWorkerSet{
					ObjectMeta: metav1.ObjectMeta{Name: "m1", Namespace: "default", ResourceVersion: "1"},
					Spec: lwsv1.LeaderWorkerSetSpec{Replicas: ptr.To[int32](1), LeaderWorkerTemplate: lwsv1.LeaderWorkerTemplate{
						LeaderTemplate: &corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "model-server", Image: "vllm/vllm:new"}}}},
						WorkerTemplate: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "model-server", Image: "vllm/vllm:new"}}}},
					}},
				},
			},
			clientObjects: []client.Object{
				&lwsv1.LeaderWorkerSet{
					ObjectMeta: metav1.ObjectMeta{Name: "m1", Namespace: "default", ResourceVersion: "1"},
					Spec: lwsv1.LeaderWorkerSetSpec{Replicas: ptr.To[int32](1), LeaderWorkerTemplate: lwsv1.LeaderWorkerTemplate{
						LeaderTemplate: &corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "model-server", Image: "vllm/vllm:old"}}}},
						WorkerTemplate: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "model-server", Image: "vllm/vllm:old"}}}},
					}},
				},
			},
			expectScaled:  true,
			expectUpgrade: true,
			expectImage:   "vllm/vllm:new",
		},
	}

	for name, tt := range specs {
		t.Run(name, func(t *testing.T) {
			// Allow nil model in helper
			if tt.plan.model == nil {
				tt.plan.model = &kubeaiv1.Model{ObjectMeta: metav1.ObjectMeta{Name: "test"}}
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(tt.clientObjects...).
				Build()

			scaled, err := tt.plan.execute(context.Background(), fakeClient, scheme)
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.expectScaled, scaled)

			if tt.expectUpgrade {
				upgraded := &lwsv1.LeaderWorkerSet{}
				err := fakeClient.Get(context.Background(), apitypes.NamespacedName{Name: tt.plan.toUpgrade.Name, Namespace: tt.plan.toUpgrade.Namespace}, upgraded)
				require.NoError(t, err)
				require.NotNil(t, upgraded.Spec.LeaderWorkerTemplate.LeaderTemplate)
				require.NotEmpty(t, upgraded.Spec.LeaderWorkerTemplate.LeaderTemplate.Spec.Containers)
				assert.Equal(t, tt.expectImage, upgraded.Spec.LeaderWorkerTemplate.LeaderTemplate.Spec.Containers[0].Image)
			}
		})
	}
}

func TestGetModelConfig_MultiNode(t *testing.T) {
	r := &ModelReconciler{
		DistributedInference: true,
		ResourceProfiles: map[string]config.ResourceProfile{
			"nvidia-gpu": {
				Requests: corev1.ResourceList{"nvidia.com/gpu": resource.MustParse("1")},
				Limits:   corev1.ResourceList{"nvidia.com/gpu": resource.MustParse("1")},
			},
		},
		ModelServers: config.ModelServers{
			VLLM: config.ModelServer{Images: map[string]string{"default": "vllm:test"}},
		},
	}

	specs := map[string]struct {
		resourceProfile string
		engine          string
		expectLWS       bool
		expectTP        int
		expectPP        int
		expectErr       bool
	}{
		"2-part profile is single-node": {
			resourceProfile: "nvidia-gpu:2",
			engine:          kubeaiv1.VLLMEngine,
			expectLWS:       false,
		},
		"3-part profile is multi-node": {
			resourceProfile: "nvidia-gpu:2:3",
			engine:          kubeaiv1.VLLMEngine,
			expectLWS:       true,
			expectTP:        2,
			expectPP:        3,
		},
		"3-part profile with non-VLLM engine fails": {
			resourceProfile: "nvidia-gpu:2:3",
			engine:          kubeaiv1.OLlamaEngine,
			expectErr:       true,
		},
		"Invalid 4-part profile fails": {
			resourceProfile: "nvidia-gpu:2:3:4",
			engine:          kubeaiv1.VLLMEngine,
			expectErr:       true,
		},
		"Invalid pp value fails": {
			resourceProfile: "nvidia-gpu:2:abc",
			engine:          kubeaiv1.VLLMEngine,
			expectErr:       true,
		},
	}

	for name, tt := range specs {
		t.Run(name, func(t *testing.T) {
			model := &kubeaiv1.Model{
				Spec: kubeaiv1.ModelSpec{
					Engine:                tt.engine,
					URL:                   "hf://test/model",
					ResourceProfile:       tt.resourceProfile,
					TargetRequests:        ptr.To[int32](100),
					ScaleDownDelaySeconds: ptr.To[int64](30),
				},
			}

			cfg, err := r.getModelConfig(model)
			if tt.expectErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			if tt.expectLWS {
				require.NotNil(t, cfg.LWSConfig)
				assert.Equal(t, tt.expectTP, cfg.LWSConfig.TensorParallelSize)
				assert.Equal(t, tt.expectPP, cfg.LWSConfig.PipelineParallelSize)
			} else {
				assert.Nil(t, cfg.LWSConfig)
			}
		})
	}
}

func TestGetModelConfig_MultiNode_DistributedInferenceDisabled(t *testing.T) {
	r := &ModelReconciler{
		DistributedInference: false,
		ResourceProfiles: map[string]config.ResourceProfile{
			"nvidia-gpu": {
				Requests: corev1.ResourceList{"nvidia.com/gpu": resource.MustParse("1")},
				Limits:   corev1.ResourceList{"nvidia.com/gpu": resource.MustParse("1")},
			},
		},
		ModelServers: config.ModelServers{
			VLLM: config.ModelServer{Images: map[string]string{"default": "vllm:test"}},
		},
	}

	model := &kubeaiv1.Model{
		Spec: kubeaiv1.ModelSpec{
			Engine:                kubeaiv1.VLLMEngine,
			URL:                   "hf://test/model",
			ResourceProfile:       "nvidia-gpu:2:3",
			TargetRequests:        ptr.To[int32](100),
			ScaleDownDelaySeconds: ptr.To[int64](30),
		},
	}

	_, err := r.getModelConfig(model)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "distributed inference is disabled")
}
