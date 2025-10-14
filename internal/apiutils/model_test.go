package apiutils_test

import (
	"testing"

	"github.com/kubeai-project/kubeai/internal/apiutils"
	"github.com/stretchr/testify/require"
)

func TestSplitModelAdapter(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		input                string
		expModel, expAdapter string
	}{
		"empty input": {
			input:      "",
			expModel:   "",
			expAdapter: "",
		},
		"model only": {
			input:    "my-model",
			expModel: "my-model",
		},
		"model and adapter": {
			input:      "my-model_my-adapter",
			expModel:   "my-model",
			expAdapter: "my-adapter",
		},
		"too many separators": {
			input:      "my-model_my-adapter_extra",
			expModel:   "my-model",
			expAdapter: "my-adapter_extra",
		},
		"trailing": {
			input:      "my-model_",
			expModel:   "my-model",
			expAdapter: "",
		},
	}

	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			model, adapter := apiutils.SplitModelAdapter(spec.input)
			require.Equal(t, spec.expModel, model, "model")
			require.Equal(t, spec.expAdapter, adapter, "adapter")
		})
	}
}

func TestMergeModelAdapter(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		model, adapter, exp string
	}{
		"model only": {
			model: "my-model",
			exp:   "my-model",
		},
		"model and adapter": {
			model:   "my-model",
			adapter: "my-adapter",
			exp:     "my-model_my-adapter",
		},
	}

	for name, spec := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			merged := apiutils.MergeModelAdapter(spec.model, spec.adapter)
			require.Equal(t, spec.exp, merged)
		})
	}
}
