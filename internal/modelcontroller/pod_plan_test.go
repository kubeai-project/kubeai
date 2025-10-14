package modelcontroller

import (
	"strings"
	"testing"
	"time"

	v1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/kubeai-project/kubeai/internal/config"
	"github.com/kubeai-project/kubeai/internal/k8sutils"
	"github.com/stretchr/testify/require"
	"golang.org/x/exp/rand"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

const (
	testNewHash = "expected-hash"
)

var (
	testYoungTS = metav1.NewTime(time.Now())
	testOldTS   = metav1.NewTime(testYoungTS.Add(-time.Hour))
)

func Test_calculatePodPlan(t *testing.T) {
	r := &ModelReconciler{
		ModelRollouts: config.ModelRollouts{
			Surge: 1,
		},
	}

	model := &v1.Model{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-mdl",
			Namespace: "test-ns",
		},
		Spec: v1.ModelSpec{
			Engine:   v1.VLLMEngine,
			Replicas: ptr.To[int32](3),
			URL:      "hf://test-repo/test-model",
		},
	}

	src, err := r.parseModelSource(model.Spec.URL)
	require.NoError(t, err)
	modelConfig := ModelConfig{
		ResourceProfile: config.ResourceProfile{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
			NodeSelector: map[string]string{
				"node": "selector",
			},
		},
		Source: src,
	}

	expectedHash := k8sutils.PodHash(r.vLLMPodForModel(model, modelConfig).Spec)

	type readiness bool
	const ready = readiness(true)
	const unready = readiness(false)

	testPod := func(name string, hash string, rdy readiness) corev1.Pod {
		p := corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
				Labels: map[string]string{
					v1.PodHashLabel: hash,
				},
			},
		}
		if rdy == ready {
			p.Status.Conditions = []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			}
		}
		return p
	}

	cases := []struct {
		name           string
		replicas       int32
		pods           []corev1.Pod
		wantNCreations int
		wantDeletions  []string
		jsonPatches    []config.JSONPatch
	}{
		{
			name: "do nothing",
			pods: []corev1.Pod{
				testPod("up-to-date-ready-1", expectedHash, ready),
				testPod("up-to-date-ready-2", expectedHash, ready),
				testPod("up-to-date-unready-3", expectedHash, unready),
			},
		},
		{
			name: "scale up",
			pods: []corev1.Pod{
				testPod("up-to-date-1", expectedHash, ready),
			},
			wantNCreations: 2,
		},
		{
			name: "scale down",
			pods: []corev1.Pod{
				testPod("ready-up-to-date-1", expectedHash, ready),
				testPod("ready-up-to-date-2", expectedHash, ready),
				testPod("unready-up-to-date", expectedHash, unready),
				testPod("ready-up-to-date-3", expectedHash, ready),
			},
			wantDeletions: []string{"unready-up-to-date"},
		},
		{
			name: "rollout add surge and delete unreadies",
			pods: []corev1.Pod{
				testPod("unready-out-of-date-1", "old-hash", unready),
				testPod("unready-out-of-date-2", "old-hash", unready),
				testPod("ready-out-of-date-3", "old-hash", ready),
			},
			wantNCreations: 1 + 2, // Expect surge Pod + 2 recreations.
			wantDeletions: []string{
				// Expect unready Pods to be deleted immediately even though all Pods are not ready.
				"unready-out-of-date-1", "unready-out-of-date-2",
			},
		},
		{
			name: "rollout wait for readiness before deleting last out of date pod",
			pods: []corev1.Pod{
				testPod("surge-pod", expectedHash, ready),
				testPod("unready-up-to-date-1", expectedHash, unready),
				testPod("unready-up-to-date-2", expectedHash, unready),
				testPod("ready-out-of-date-3", "old-hash", ready),
			},
		},
		{
			name: "rollout delete ready out of date pod",
			pods: []corev1.Pod{
				testPod("surge-pod", expectedHash, ready),
				testPod("unready-up-to-date-1", expectedHash, ready),
				testPod("unready-up-to-date-2", expectedHash, ready),
				testPod("ready-out-of-date-3", "old-hash", ready),
			},
			wantNCreations: 0,
			wantDeletions:  []string{"ready-out-of-date-3"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r.ModelServerPods.JSONPatches = nil
			if c.jsonPatches != nil {
				r.ModelServerPods.JSONPatches = c.jsonPatches
			}
			plan, err := r.calculatePodPlan(&corev1.PodList{Items: c.pods}, model, modelConfig)
			require.NoError(t, err)
			detailsCSV := strings.Join(plan.details, ", ")
			require.Lenf(t, plan.toCreate, c.wantNCreations, "Unexpected creation count, details: %v", detailsCSV)
			var deletionNames []string
			for _, p := range plan.toDelete {
				deletionNames = append(deletionNames, p.Name)
			}
			require.Lenf(t, deletionNames, len(c.wantDeletions), "Unexpected deletion count, details: %v", detailsCSV)
			require.Equalf(t, c.wantDeletions, deletionNames, "Unexpected deleteion names, details: %v", detailsCSV)
		})
	}
}

func Test_sortPodsByDeletionOrder(t *testing.T) {
	cases := []struct {
		name string
		pods []corev1.Pod
		want []string
	}{
		{
			name: "empty",
			pods: []corev1.Pod{},
			want: nil,
		},
		{
			name: "hash comparison",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "expected-hash-pod",
						Labels: map[string]string{
							v1.PodHashLabel: testNewHash,
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "old-hash-pod",
						Labels: map[string]string{
							v1.PodHashLabel: "old-hash",
						},
					},
				},
			},
			want: []string{
				"old-hash-pod",
				"expected-hash-pod",
			},
		},
		{
			name: "ready comparison",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "ready-pod",
					},
					Status: corev1.PodStatus{
						Conditions: []corev1.PodCondition{
							{
								Type:   corev1.PodReady,
								Status: corev1.ConditionTrue,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "not-ready-pod",
					},
				},
			},
			want: []string{
				"not-ready-pod",
				"ready-pod",
			},
		},
		{
			name: "scheduled comparison",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "scheduled-pod",
					},
					Spec: corev1.PodSpec{
						NodeName: "node",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "unscheduled-pod",
					},
				},
			},
			want: []string{
				"unscheduled-pod",
				"scheduled-pod",
			},
		},
		{
			name: "creation time comparison",
			pods: []corev1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "old-pod",
						CreationTimestamp: testOldTS,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "young-pod",
						CreationTimestamp: testYoungTS,
					},
				},
			},
			want: []string{
				"young-pod",
				"old-pod",
			},
		},
		{
			name: "all",
			pods: []corev1.Pod{
				youngReadyScheduledOldHashPod(),
				youngUnreadyScheduledNewHashPod(),
				youngUnreadyUnscheduledOldHashPod(),
				oldUnreadyUnscheduledOldHashPod(),
				youngUnreadyScheduledOldHashPod(),
				youngReadyScheduledNewHashPod(),
				oldUnreadyScheduledNewHashPod(),
			},
			want: []string{
				youngUnreadyUnscheduledOldHashPod().Name,
				oldUnreadyUnscheduledOldHashPod().Name,
				youngUnreadyScheduledOldHashPod().Name,
				youngUnreadyScheduledNewHashPod().Name,
				oldUnreadyScheduledNewHashPod().Name,
				youngReadyScheduledOldHashPod().Name,
				youngReadyScheduledNewHashPod().Name,
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Run a lot of times with random input ordering.
			for i := 0; i < 10000; i++ {
				// Copy the slice to avoid modifying the original slice.
				pods := append([]corev1.Pod(nil), c.pods...)

				randomizePodOrder(pods)

				sortPodsByDeletionOrder(pods, testNewHash)

				var namesAfter []string
				for _, p := range pods {
					namesAfter = append(namesAfter, p.Name)
				}
				require.Equal(t, c.want, namesAfter)
			}
		})
	}
}

func randomizePodOrder(pods []corev1.Pod) {
	for i := range pods {
		j := rand.Intn(len(pods))
		pods[i], pods[j] = pods[j], pods[i]
	}
}

func youngReadyScheduledNewHashPod() corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "young-ready-scheduled-new-hash-pod",
			Labels: map[string]string{
				v1.PodHashLabel: testNewHash,
			},
			CreationTimestamp: testYoungTS,
		},
		Spec: corev1.PodSpec{
			NodeName: "node",
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
}

func youngReadyScheduledOldHashPod() corev1.Pod {

	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "young-ready-scheduled-old-hash-pod",
			Labels: map[string]string{
				v1.PodHashLabel: "old-hash",
			},
			CreationTimestamp: testYoungTS,
		},
		Spec: corev1.PodSpec{
			NodeName: "node",
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
}

func youngUnreadyScheduledOldHashPod() corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "young-unready-scheduled-old-hash-pod",
			Labels: map[string]string{
				v1.PodHashLabel: "old-hash",
			},
			CreationTimestamp: testYoungTS,
		},
		Spec: corev1.PodSpec{
			NodeName: "node",
		},
	}
}

func oldUnreadyScheduledNewHashPod() corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "old-unready-scheduled-new-hash-pod",
			Labels: map[string]string{
				v1.PodHashLabel: testNewHash,
			},
			CreationTimestamp: testOldTS,
		},
		Spec: corev1.PodSpec{
			NodeName: "node",
		},
	}
}

func youngUnreadyScheduledNewHashPod() corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "young-unready-scheduled-new-hash-pod",
			Labels: map[string]string{
				v1.PodHashLabel: testNewHash,
			},
			CreationTimestamp: testYoungTS,
		},
		Spec: corev1.PodSpec{
			NodeName: "node",
		},
	}
}

func youngUnreadyUnscheduledOldHashPod() corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "young-unready-unscheduled-old-hash-pod",
			Labels: map[string]string{
				v1.PodHashLabel: "old-hash",
			},
			CreationTimestamp: testYoungTS,
		},
	}
}

func oldUnreadyUnscheduledOldHashPod() corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "old-unready-unscheduled-old-hash-pod",
			Labels: map[string]string{
				v1.PodHashLabel: "old-hash",
			},
			CreationTimestamp: testOldTS,
		},
	}
}
