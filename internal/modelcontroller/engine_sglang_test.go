package modelcontroller

import (
	"testing"

	kubeaiv1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_sGLangPodForModel(t *testing.T) {
	t.Parallel()

	const modelName = "model-name"

	cases := map[string]struct {
		url          string
		cacheProfile string
		specArgs     []string
		wantArgs     []string
	}{
		"hf": {
			url: "hf://Qwen/Qwen2.5-0.5B-Instruct",
			wantArgs: []string{
				"--model-path=Qwen/Qwen2.5-0.5B-Instruct",
				"--served-model-name=" + modelName,
				"--host=0.0.0.0",
				"--port=8000",
			},
		},
		"pvc": {
			url: "pvc://my-pvc/models/qwen",
			wantArgs: []string{
				"--model-path=/model",
				"--served-model-name=" + modelName,
				"--host=0.0.0.0",
				"--port=8000",
			},
		},
		"oci": {
			url: "oci://registry.example.com/repo:tag",
			wantArgs: []string{
				"--model-path=/model",
				"--served-model-name=" + modelName,
				"--host=0.0.0.0",
				"--port=8000",
			},
		},
		"spec-args-appended-last": {
			url:      "hf://Qwen/Qwen2.5-0.5B-Instruct",
			specArgs: []string{"--tp-size=2", "--mem-fraction-static=0.9"},
			wantArgs: []string{
				"--model-path=Qwen/Qwen2.5-0.5B-Instruct",
				"--served-model-name=" + modelName,
				"--host=0.0.0.0",
				"--port=8000",
				"--tp-size=2",
				"--mem-fraction-static=0.9",
			},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := &ModelReconciler{}
			src, err := r.parseModelSource(c.url)
			require.NoError(t, err)
			m := &kubeaiv1.Model{
				ObjectMeta: metav1.ObjectMeta{Name: modelName, Namespace: "default"},
				Spec: kubeaiv1.ModelSpec{
					URL:          c.url,
					Engine:       kubeaiv1.SGLangEngine,
					CacheProfile: c.cacheProfile,
					Features:     []kubeaiv1.ModelFeature{kubeaiv1.ModelFeatureTextGeneration},
					Args:         c.specArgs,
					Env:          map[string]string{"B_VAR": "b", "A_VAR": "a"},
				},
			}
			pod := r.sGLangPodForModel(m, ModelConfig{Source: src})

			ctr := pod.Spec.Containers[0]
			require.Equal(t, []string{"python3", "-m", "sglang.launch_server"}, ctr.Command)
			require.Equal(t, c.wantArgs, ctr.Args)
			require.Equal(t, "8000", pod.Annotations[kubeaiv1.ModelPodPortAnnotation])
			require.Equal(t, "/health", ctr.ReadinessProbe.HTTPGet.Path)
			// Env keys are sorted so the Pod hash stays stable across reconciles.
			require.Equal(t, "A_VAR", ctr.Env[0].Name)
			require.Equal(t, "B_VAR", ctr.Env[1].Name)
		})
	}
}

func Test_sGLangFeatureArgs(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		features []kubeaiv1.ModelFeature
		want     []string
	}{
		"none":            {features: nil, want: nil},
		"text-generation": {features: []kubeaiv1.ModelFeature{kubeaiv1.ModelFeatureTextGeneration}, want: nil},
		"embedding":       {features: []kubeaiv1.ModelFeature{kubeaiv1.ModelFeatureTextEmbedding}, want: []string{"--is-embedding"}},
		"reranking":       {features: []kubeaiv1.ModelFeature{kubeaiv1.ModelFeatureReranking}, want: []string{"--is-embedding"}},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, c.want, sGLangFeatureArgs(c.features))
		})
	}
}
