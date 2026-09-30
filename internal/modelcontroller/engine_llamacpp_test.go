package modelcontroller

import (
	"testing"

	kubeaiv1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_llamaCppModelArgs(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		url  string
		want []string
	}{
		"hf-repo": {
			url:  "hf://Qwen/Qwen2.5-0.5B-Instruct-GGUF",
			want: []string{"-hf", "Qwen/Qwen2.5-0.5B-Instruct-GGUF"},
		},
		"hf-repo-with-quant": {
			url:  "hf://Qwen/Qwen2.5-0.5B-Instruct-GGUF:q4_k_m",
			want: []string{"-hf", "Qwen/Qwen2.5-0.5B-Instruct-GGUF:q4_k_m"},
		},
		"pvc-dir-with-model-param": {
			url:  "pvc://my-pvc/gguf?model=model.gguf",
			want: []string{"-m", "/model/model.gguf"},
		},
		"pvc-dir-with-sharded-model-param": {
			url:  "pvc://my-pvc/gguf?model=stories15M-00001-of-00003.gguf",
			want: []string{"-m", "/model/stories15M-00001-of-00003.gguf"},
		},
		"pvc-single-file": {
			url:  "pvc://my-pvc/gguf/model.gguf",
			want: []string{"-m", "/model"},
		},
		"oci-with-model-param": {
			url:  "oci://registry.example.com/repo:tag?model=model.gguf",
			want: []string{"-m", "/model/model.gguf"},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			u, err := parseModelURL(c.url)
			require.NoError(t, err)
			require.Equal(t, c.want, llamaCppModelArgs(u))
		})
	}
}

func Test_llamaCppFeatureArgs(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		features []kubeaiv1.ModelFeature
		want     []string
	}{
		"none":            {features: nil, want: nil},
		"text-generation": {features: []kubeaiv1.ModelFeature{kubeaiv1.ModelFeatureTextGeneration}, want: nil},
		"embedding":       {features: []kubeaiv1.ModelFeature{kubeaiv1.ModelFeatureTextEmbedding}, want: []string{"--embedding"}},
		"reranking":       {features: []kubeaiv1.ModelFeature{kubeaiv1.ModelFeatureReranking}, want: []string{"--reranking"}},
		"reranking-wins-over-embedding": {
			features: []kubeaiv1.ModelFeature{kubeaiv1.ModelFeatureTextEmbedding, kubeaiv1.ModelFeatureReranking},
			want:     []string{"--reranking"},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, c.want, llamaCppFeatureArgs(c.features))
		})
	}
}

func Test_validateLlamaCppModel(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		url      string
		features []kubeaiv1.ModelFeature
		wantErr  bool
	}{
		"valid-text-generation": {
			url:      "pvc://my-pvc/gguf?model=model.gguf",
			features: []kubeaiv1.ModelFeature{kubeaiv1.ModelFeatureTextGeneration},
		},
		"valid-embedding": {
			url:      "hf://ggml-org/bge-small-en-v1.5-Q8_0-GGUF",
			features: []kubeaiv1.ModelFeature{kubeaiv1.ModelFeatureTextEmbedding},
		},
		"model-param-traversal": {
			url:      "pvc://my-pvc/gguf?model=../../etc/shadow",
			features: []kubeaiv1.ModelFeature{kubeaiv1.ModelFeatureTextGeneration},
			wantErr:  true,
		},
		"model-param-absolute": {
			url:      "pvc://my-pvc/gguf?model=/etc/shadow",
			features: []kubeaiv1.ModelFeature{kubeaiv1.ModelFeatureTextGeneration},
			wantErr:  true,
		},
		"speech-to-text-unsupported": {
			url:      "pvc://my-pvc/gguf?model=model.gguf",
			features: []kubeaiv1.ModelFeature{kubeaiv1.ModelFeatureSpeechToText},
			wantErr:  true,
		},
		"oci-without-model-param": {
			url:      "oci://registry.example.com/repo:tag",
			features: []kubeaiv1.ModelFeature{kubeaiv1.ModelFeatureTextGeneration},
			wantErr:  true,
		},
		"pvc-directory-without-model-param": {
			url:      "pvc://my-pvc/gguf",
			features: []kubeaiv1.ModelFeature{kubeaiv1.ModelFeatureTextGeneration},
			wantErr:  true,
		},
		"pvc-file-without-model-param": {
			url:      "pvc://my-pvc/gguf/model.gguf",
			features: []kubeaiv1.ModelFeature{kubeaiv1.ModelFeatureTextGeneration},
		},
		"hf-without-model-param": {
			url:      "hf://Qwen/Qwen2.5-0.5B-Instruct-GGUF:q4_k_m",
			features: []kubeaiv1.ModelFeature{kubeaiv1.ModelFeatureTextGeneration},
		},
		"generation-and-embedding-conflict": {
			url:      "pvc://my-pvc/gguf?model=model.gguf",
			features: []kubeaiv1.ModelFeature{kubeaiv1.ModelFeatureTextGeneration, kubeaiv1.ModelFeatureTextEmbedding},
			wantErr:  true,
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := &kubeaiv1.Model{
				ObjectMeta: metav1.ObjectMeta{Name: "model-name"},
				Spec: kubeaiv1.ModelSpec{
					URL:      c.url,
					Engine:   kubeaiv1.LlamaCppEngine,
					Features: c.features,
				},
			}
			err := validateLlamaCppModel(m)
			if c.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}
