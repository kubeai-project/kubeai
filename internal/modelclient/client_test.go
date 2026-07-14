package modelclient

import (
	"context"
	"testing"

	kubeaiv1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const operatorNS = "kubeai-system"

func newTestClient(t *testing.T, models ...*kubeaiv1.Model) *ModelClient {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(scheme))
	require.NoError(t, kubeaiv1.AddToScheme(scheme))
	builder := fake.NewClientBuilder().WithScheme(scheme)
	for _, m := range models {
		builder = builder.WithObjects(m)
	}
	return NewModelClient(builder.Build(), operatorNS)
}

func model(ns, name string) *kubeaiv1.Model {
	return &kubeaiv1.Model{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name},
		Spec: kubeaiv1.ModelSpec{
			URL:             "hf://x/y",
			Engine:          "VLLM",
			Features:        []kubeaiv1.ModelFeature{kubeaiv1.ModelFeatureTextGeneration},
			ResourceProfile: "cpu:1",
		},
	}
}

func TestLookupModel_NotFound(t *testing.T) {
	c := newTestClient(t)
	m, err := c.LookupModel(context.Background(), "missing", "", nil)
	require.NoError(t, err)
	require.Nil(t, m)
}

func TestLookupModel_SingleNamespace(t *testing.T) {
	c := newTestClient(t, model(operatorNS, "llama"))
	m, err := c.LookupModel(context.Background(), "llama", "", nil)
	require.NoError(t, err)
	require.NotNil(t, m)
	require.Equal(t, operatorNS, m.Namespace)
}

func TestLookupModel_CrossNamespace(t *testing.T) {
	c := newTestClient(t, model("team-a", "llama"))
	m, err := c.LookupModel(context.Background(), "llama", "", nil)
	require.NoError(t, err)
	require.NotNil(t, m)
	require.Equal(t, "team-a", m.Namespace)
}

func TestLookupModel_CollisionPrefersOperatorNamespace(t *testing.T) {
	c := newTestClient(t,
		model("team-a", "llama"),
		model(operatorNS, "llama"),
		model("team-b", "llama"),
	)
	m, err := c.LookupModel(context.Background(), "llama", "", nil)
	require.NoError(t, err)
	require.NotNil(t, m)
	require.Equal(t, operatorNS, m.Namespace, "match in operator namespace should win")
}

func TestLookupModel_CollisionAlphabeticalFallback(t *testing.T) {
	// No match in operator namespace; alphabetically-first namespace wins.
	c := newTestClient(t,
		model("team-c", "llama"),
		model("team-a", "llama"),
		model("team-b", "llama"),
	)
	m, err := c.LookupModel(context.Background(), "llama", "", nil)
	require.NoError(t, err)
	require.NotNil(t, m)
	require.Equal(t, "team-a", m.Namespace)
}

func TestListAllModels_AcrossNamespaces(t *testing.T) {
	c := newTestClient(t,
		model(operatorNS, "a"),
		model("team-a", "b"),
		model("team-b", "c"),
	)
	models, err := c.ListAllModels(context.Background())
	require.NoError(t, err)
	require.Len(t, models, 3)
}
