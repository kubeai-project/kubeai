package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
	lwsv1 "sigs.k8s.io/lws/api/leaderworkerset/v1"

	"github.com/kubeai-project/kubeai/internal/modelcontroller"
)

func TestLWSMultiNode(t *testing.T) {
	sysCfg := baseSysCfg(t)
	sysCfg.DistributedInference = true
	initTest(t, sysCfg)

	m := modelForTest(t)
	// 3-part resource profile triggers LWS mode: <name>:<tp>:<pp>
	m.Spec.ResourceProfile = resourceProfileCPU + ":1:2"
	m.Spec.MinReplicas = 1
	require.NoError(t, testK8sClient.Create(testCtx, m))

	// The model controller should create a LeaderWorkerSet.
	// In envtest, the LWS controller is NOT running so no pods will be created,
	// but we can verify the LWS object itself.
	var lws lwsv1.LeaderWorkerSet
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		lwsName := strings.ReplaceAll(m.Name, ".", "-")
		err := testK8sClient.Get(testCtx, client.ObjectKey{
			Name:      lwsName,
			Namespace: testNS,
		}, &lws)
		assert.NoError(ct, err, "LeaderWorkerSet should be created by model controller")
	}, 10*time.Second, 200*time.Millisecond, "waiting for LWS creation")

	// Verify LWS spec
	require.NotNil(t, lws.Spec.Replicas, "LWS replicas should be set")
	assert.Equal(t, int32(1), *lws.Spec.Replicas, "LWS should have 1 replica")
	require.NotNil(t, lws.Spec.LeaderWorkerTemplate.Size, "LWS group size should be set")
	assert.Equal(t, int32(2), *lws.Spec.LeaderWorkerTemplate.Size, "LWS group size should match pipeline-parallel (2)")

	// Verify head pod has the model label and group-role label
	headLabels := lws.Spec.LeaderWorkerTemplate.LeaderTemplate.Labels
	assert.Equal(t, m.Name, headLabels["model"], "head should carry model label")
	assert.Equal(t, modelcontroller.GroupRoleHead, headLabels[modelcontroller.LabelGroupRole], "head should be labeled as head")

	// Verify worker pod does NOT have the model label (to prevent load balancer routing)
	workerLabels := lws.Spec.LeaderWorkerTemplate.WorkerTemplate.Labels
	_, hasModel := workerLabels["model"]
	assert.False(t, hasModel, "worker should NOT carry model label")
	assert.Equal(t, modelcontroller.GroupRoleWorker, workerLabels[modelcontroller.LabelGroupRole], "worker should be labeled as worker")

	// Verify the head pod has TP/PP args
	headArgs := lws.Spec.LeaderWorkerTemplate.LeaderTemplate.Spec.Containers[0].Args
	assert.Contains(t, headArgs, "--tensor-parallel-size=1")
	assert.Contains(t, headArgs, "--pipeline-parallel-size=2")
}

func TestLWSMultiNodeUpdateRollout(t *testing.T) {
	sysCfg := baseSysCfg(t)
	sysCfg.DistributedInference = true
	initTest(t, sysCfg)

	m := modelForTest(t)
	// 3-part resource profile triggers LWS mode: <name>:<tp>:<pp>
	m.Spec.ResourceProfile = resourceProfileCPU + ":1:2"
	m.Spec.MinReplicas = 1
	require.NoError(t, testK8sClient.Create(testCtx, m))

	// Update the Model object.
	const newArg = "--my-new-arg-added-in-testcase"
	updateModel(t, m, func() { m.Spec.Args = []string{newArg} }, "Adding a new arg to the Model")

	// The model controller should create a LeaderWorkerSet.
	// In envtest, the LWS controller is NOT running so no pods will be created,
	// but we can verify the LWS object itself.
	var lws lwsv1.LeaderWorkerSet
	require.EventuallyWithT(t, func(ct *assert.CollectT) {
		lwsName := strings.ReplaceAll(m.Name, ".", "-")
		err := testK8sClient.Get(testCtx, client.ObjectKey{
			Name:      lwsName,
			Namespace: testNS,
		}, &lws)
		assert.NoError(ct, err, "LeaderWorkerSet should be created by model controller")
	}, 10*time.Second, 200*time.Millisecond, "waiting for LWS creation")

	assert.Containsf(t, lws.Spec.LeaderWorkerTemplate.LeaderTemplate.Spec.Containers[0].Args, newArg, "head should have the new arg")
	assert.NotContainsf(t, lws.Spec.LeaderWorkerTemplate.WorkerTemplate.Spec.Containers[0].Args, newArg, "worker should NOT have the new arg")
}
