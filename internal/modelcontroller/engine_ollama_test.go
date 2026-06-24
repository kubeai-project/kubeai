// TODO: write tests
package modelcontroller

import (
	"testing"

	kubeaiv1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_ollamaStartupProbeScript(t *testing.T) {
	t.Parallel()

	modelName := "model-name"
	ollamaRef := "qwen2:0.5b"

	cases := map[string]struct {
		model       kubeaiv1.Model
		modelURL    modelURL
		featuresMap map[kubeaiv1.ModelFeature]struct{}
		want        []string
	}{
		"basic-model-no-pvc": {
			model: kubeaiv1.Model{
				ObjectMeta: metav1.ObjectMeta{
					Name: modelName,
				},
				Spec: kubeaiv1.ModelSpec{
					Features: []kubeaiv1.ModelFeature{kubeaiv1.ModelFeatureTextGeneration},
				},
			},
			modelURL: modelURL{
				scheme: "ollama",
				ref:    ollamaRef,
				name:   "abc",
				pull:   true, // models pull by default
			},
			want: []string{"/bin/ollama", "pull", ollamaRef, "&&", "/bin/ollama", "cp", ollamaRef, modelName, "&&", "/bin/ollama", "run", modelName, "hi"},
		},
		"basic-model-with-pvc": {
			model: kubeaiv1.Model{
				ObjectMeta: metav1.ObjectMeta{
					Name: modelName,
				},
				Spec: kubeaiv1.ModelSpec{
					Features: []kubeaiv1.ModelFeature{kubeaiv1.ModelFeatureTextGeneration},
				},
			},
			modelURL: modelURL{
				scheme:     "pvc",
				ref:        "def",
				name:       "abc",
				modelParam: ollamaRef,
				pull:       true, // models pull by default
			},
			want: []string{"/bin/ollama", "cp", ollamaRef, modelName, "&&", "/bin/ollama", "run", modelName, "hi"},
		},
		"insecure-pull-no-pvc": {
			model: kubeaiv1.Model{
				ObjectMeta: metav1.ObjectMeta{
					Name: modelName,
				},
				Spec: kubeaiv1.ModelSpec{
					Features: []kubeaiv1.ModelFeature{kubeaiv1.ModelFeatureTextGeneration},
				},
			},
			modelURL: modelURL{
				scheme:   "ollama",
				ref:      ollamaRef,
				name:     "abc",
				insecure: true, // Set insecure flag here
				pull:     true, // models pull by default
			},
			want: []string{"/bin/ollama", "pull", "--insecure", ollamaRef, "&&", "/bin/ollama", "cp", ollamaRef, modelName, "&&", "/bin/ollama", "run", modelName, "hi"},
		},
		"no-pull-no-pvc": {
			model: kubeaiv1.Model{
				ObjectMeta: metav1.ObjectMeta{
					Name: modelName,
				},
				Spec: kubeaiv1.ModelSpec{
					Features: []kubeaiv1.ModelFeature{kubeaiv1.ModelFeatureTextGeneration},
				},
			},
			modelURL: modelURL{
				scheme: "ollama",
				ref:    ollamaRef,
				name:   "abc",
				pull:   false, // Set pull to false
			},
			want: []string{"/bin/ollama", "cp", ollamaRef, modelName, "&&", "/bin/ollama", "run", modelName, "hi"},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := ollamaStartupProbeScript(&c.model, c.modelURL)
			require.Equal(t, c.want, got)
		})
	}
}
