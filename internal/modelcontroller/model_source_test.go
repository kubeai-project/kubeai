package modelcontroller

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func Test_parseModelURL(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		name    string
		input   string
		want    modelURL
		wantErr bool
	}{
		"empty": {
			input:   "",
			wantErr: true,
		},
		"invalid-scheme": {
			input:   "iNv@lid://path/to/model",
			wantErr: true,
		},
		"double-scheme-edge-case": {
			input: "a://path/b://to/model",
			want: modelURL{
				scheme: "a",
				ref:    "path/b://to/model",
				name:   "path",
				path:   "b://to/model",
				pull:   true,
			},
		},
		"valid-google-storage": {
			input: "gs://bucket-name/path/to/model",
			want: modelURL{
				scheme: "gs",
				ref:    "bucket-name/path/to/model",
				name:   "bucket-name",
				path:   "path/to/model",
				pull:   true,
			},
		},
		"valid-ollama": {
			input: "ollama://gemma2:2b",
			want: modelURL{
				scheme: "ollama",
				ref:    "gemma2:2b",
				name:   "gemma2:2b",
				path:   "",
				pull:   true,
			},
		},
		"valid-huggingface": {
			input: "hf://test-user/model-name",
			want: modelURL{
				scheme: "hf",
				ref:    "test-user/model-name",
				name:   "test-user",
				path:   "model-name",
				pull:   true,
			},
		},
		"valid-s3": {
			input: "s3://test-bucket/model-name",
			want: modelURL{
				scheme: "s3",
				ref:    "test-bucket/model-name",
				name:   "test-bucket",
				path:   "model-name",
				pull:   true,
			},
		},
		"valid-pvc": {
			input: "pvc://my-vpc/path/to/model",
			want: modelURL{
				scheme: "pvc",
				ref:    "my-vpc/path/to/model",
				name:   "my-vpc",
				path:   "path/to/model",
				pull:   true,
			},
		},
		"valid-pvc-no-path": {
			input: "pvc://my-vpc",
			want: modelURL{
				scheme: "pvc",
				ref:    "my-vpc",
				name:   "my-vpc",
				path:   "",
				pull:   true,
			},
		},
		"valid-pvc-with-slash-empty": {
			input: "pvc://my-vpc/",
			want: modelURL{
				scheme: "pvc",
				ref:    "my-vpc/",
				name:   "my-vpc",
				path:   "",
				pull:   true,
			},
		},
		"valid-pvc-with-double-slash": {
			input: "pvc://my-vpc//",
			want: modelURL{
				scheme: "pvc",
				ref:    "my-vpc//",
				name:   "my-vpc",
				path:   "/",
				pull:   true,
			},
		},
		"valid-pvc-with-modelname": {
			input: "pvc://my-vpc?model=qwen2:0.5b",
			want: modelURL{
				scheme:     "pvc",
				ref:        "my-vpc",
				name:       "my-vpc",
				path:       "",
				modelParam: "qwen2:0.5b",
				pull:       true,
			},
		},
		"valid-pvc-withpath-and-modelname": {
			input: "pvc://my-vpc/path/to/model?model=qwen2:0.5b",
			want: modelURL{
				scheme:     "pvc",
				ref:        "my-vpc/path/to/model",
				name:       "my-vpc",
				path:       "path/to/model",
				modelParam: "qwen2:0.5b",
				pull:       true,
			},
		},
		"valid-ollama-with-no-pull": {
			input: "ollama://gemma2:2b?pull=false",
			want: modelURL{
				scheme: "ollama",
				ref:    "gemma2:2b",
				name:   "gemma2:2b",
				path:   "",
				pull:   false,
			},
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := parseModelURL(c.input)
			if c.wantErr {
				require.Error(t, err)
				return
			} else {
				require.NoError(t, err)
			}
			c.want.original = c.input
			require.Equal(t, c.want, got)
		})
	}
}

func Test_ociPodAdditions(t *testing.T) {
	t.Parallel()

	r := &ModelReconciler{}
	r.ModelLoaders.Llmman = "ghcr.io/kubeai-project/kubeai-llmman-loader:test"

	url, err := parseModelURL("oci://ghcr.io/org/model:tag")
	require.NoError(t, err)
	additions := r.ociPodAdditions(url)

	// An emptyDir, not an ImageVolume: containerd cannot mount a plain OCI
	// artifact as an image volume, so the bytes are pulled into a volume the
	// model container reads.
	require.Len(t, additions.volumes, 1)
	require.NotNil(t, additions.volumes[0].EmptyDir)
	require.Nil(t, additions.volumes[0].Image)

	require.Len(t, additions.initContainers, 1)
	puller := additions.initContainers[0]
	require.Equal(t, "ghcr.io/kubeai-project/kubeai-llmman-loader:test", puller.Image)
	// The whole reference is handed to the puller, and the mount path matches
	// what the model container expects.
	require.Equal(t, []string{"ghcr.io/org/model:tag", "/model"}, puller.Args)
	require.Equal(t, "/model", puller.VolumeMounts[0].MountPath)

	require.Len(t, additions.volumeMounts, 1)
	require.Equal(t, "/model", additions.volumeMounts[0].MountPath)
	require.True(t, additions.volumeMounts[0].ReadOnly)

	// The daemon address is passed through, defaulted when unset.
	var host string
	for _, env := range puller.Env {
		if env.Name == "LLMMAN_HOST" {
			host = env.Value
		}
	}
	require.Equal(t, defaultLlmmanHost, host)

	// No image pull secret: registry credentials live on the llmman daemon.
	require.Empty(t, additions.imagePullSecrets)
}

func Test_llmmanHost(t *testing.T) {
	t.Parallel()

	r := &ModelReconciler{}
	require.Equal(t, defaultLlmmanHost, r.llmmanHost())

	// A cluster can point every model pod at one shared daemon.
	r.ModelLoaders.LlmmanHost = "llmman.kubeai.svc:17434"
	require.Equal(t, "llmman.kubeai.svc:17434", r.llmmanHost())

	r.ModelLoaders.LlmmanHost = "   "
	require.Equal(t, defaultLlmmanHost, r.llmmanHost())
}

func Test_applyToPodSpecAddsInitContainers(t *testing.T) {
	t.Parallel()

	spec := &corev1.PodSpec{Containers: []corev1.Container{{Name: "server"}}}
	additions := &modelSourcePodAdditions{
		initContainers: []corev1.Container{{Name: "model-puller"}},
	}
	additions.applyToPodSpec(spec, 0)

	require.Len(t, spec.InitContainers, 1)
	require.Equal(t, "model-puller", spec.InitContainers[0].Name)
}
