// TODO: write tests
package modelcontroller

import (
	"fmt"
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
		want        string
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
			want: fmt.Sprintf("/bin/ollama pull %s && /bin/ollama cp %s %s && /bin/ollama run %s hi",
				ollamaRef, ollamaRef, modelName, modelName),
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
			want: fmt.Sprintf("/bin/ollama cp %s %s && /bin/ollama run %s hi",
				ollamaRef, modelName, modelName),
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
			want: fmt.Sprintf("/bin/ollama pull --insecure %s && /bin/ollama cp %s %s && /bin/ollama run %s hi",
				ollamaRef, ollamaRef, modelName, modelName),
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
			want: fmt.Sprintf("/bin/ollama cp %s %s && /bin/ollama run %s hi",
				ollamaRef, modelName, modelName),
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
