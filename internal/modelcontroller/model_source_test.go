package modelcontroller

import (
	"strings"
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
		"valid-oci": {
			input: "oci://ghcr.io/org/model:tag",
			want: modelURL{
				scheme: "oci",
				ref:    "ghcr.io/org/model:tag",
				name:   "ghcr.io",
				path:   "org/model:tag",
				pull:   true,
			},
		},
		"valid-oci-via-llmman": {
			input: "oci://ghcr.io/org/model:tag?via=llmman",
			want: modelURL{
				scheme:    "oci",
				ref:       "ghcr.io/org/model:tag",
				name:      "ghcr.io",
				path:      "org/model:tag",
				pull:      true,
				viaLlmman: true,
			},
		},
		"invalid-via": {
			input:   "oci://ghcr.io/org/model:tag?via=other",
			wantErr: true,
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

func newOCIReconciler() *ModelReconciler {
	r := &ModelReconciler{}
	r.SecretNames.OCI = "oci-secret"
	r.ModelLoaders.Llmman = "ghcr.io/kubeai-project/kubeai-llmman-loader:test"
	r.ModelLoaders.LlmmanStore = "llmman-store"
	return r
}

func Test_parseModelSourceOCI(t *testing.T) {
	t.Parallel()

	r := newOCIReconciler()

	// Default: a kubelet ImageVolume with the image pull secret.
	src, err := r.parseModelSource("oci://ghcr.io/org/model:tag")
	require.NoError(t, err)
	require.Len(t, src.volumes, 1)
	require.NotNil(t, src.volumes[0].Image)
	require.Equal(t, "ghcr.io/org/model:tag", src.volumes[0].Image.Reference)
	require.Equal(t, []corev1.LocalObjectReference{{Name: "oci-secret"}}, src.imagePullSecrets)
	require.Empty(t, src.initContainers)
	require.Equal(t, modelMountPath, src.volumeMounts[0].MountPath)
	require.False(t, src.volumeMounts[0].ReadOnly)

	// Opt-in: an init container pulls through llmman into an emptyDir.
	src, err = r.parseModelSource("oci://ghcr.io/org/model:tag?via=llmman")
	require.NoError(t, err)
	require.Len(t, src.volumes, 2)
	require.NotNil(t, src.volumes[0].EmptyDir)
	require.Nil(t, src.volumes[0].Image)
	require.Equal(t, "llmman-store", src.volumes[1].PersistentVolumeClaim.ClaimName)
	require.Empty(t, src.imagePullSecrets)
	require.Equal(t, modelMountPath, src.volumeMounts[0].MountPath)
	require.True(t, src.volumeMounts[0].ReadOnly)

	require.Len(t, src.initContainers, 1)
	puller := src.initContainers[0]
	require.Equal(t, r.ModelLoaders.Llmman, puller.Image)
	// The opt-in marker is stripped from the reference.
	require.Equal(t, []string{"ghcr.io/org/model:tag", modelMountPath}, puller.Args)
	require.Equal(t, []corev1.EnvVar{
		{Name: "LLMMAN_HOST", Value: defaultLlmmanHost},
		{Name: "LLMMAN_MODELS", Value: "/llmman/store"},
	}, puller.Env)
	require.Equal(t, []corev1.VolumeMount{
		{Name: modelVolumeName, MountPath: modelMountPath},
		{Name: "llmman-store", MountPath: "/llmman"},
	}, puller.VolumeMounts)
}

func Test_parseModelSourceViaLlmmanNeedsConfig(t *testing.T) {
	t.Parallel()

	for name, mutate := range map[string]func(*ModelReconciler){
		"no-image": func(r *ModelReconciler) { r.ModelLoaders.Llmman = "" },
		"no-store": func(r *ModelReconciler) { r.ModelLoaders.LlmmanStore = "" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := newOCIReconciler()
			mutate(r)
			_, err := r.parseModelSource("oci://ghcr.io/org/model:tag?via=llmman")
			require.Error(t, err)
			// The default path needs neither.
			_, err = r.parseModelSource("oci://ghcr.io/org/model:tag")
			require.NoError(t, err)
		})
	}
}

func Test_llmmanHost(t *testing.T) {
	t.Parallel()

	r := &ModelReconciler{}
	require.Equal(t, defaultLlmmanHost, r.llmmanHost())

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

func TestModelURLRejectsShellSyntax(t *testing.T) {
	t.Parallel()
	// Both the reference and the decoded PVC model parameter must keep #656's
	// validation boundary. Encoded shell syntax is not a safe model name.
	for _, value := range []string{
		"model;id", "model&&id", "model|id", "$(id)", "`id`",
		"model name", "model\tname", "model\nname", "model'name", `model"name`,
		"model>file", "model<file", "model\\name",
		"model%3Bid", "%24%28id%29", "%60id%60", "model%20name", "model%0Aname",
	} {
		for _, prefix := range []string{"ollama://", "pvc://models?model="} {
			t.Run(prefix+value, func(t *testing.T) {
				t.Parallel()
				input := value
				if prefix == "pvc://models?model=" {
					// Raw & separates query parameters; encode it to test an
					// ampersand in the model value itself.
					input = strings.ReplaceAll(input, "&", "%26")
				}
				_, err := parseModelURL(prefix + input)
				require.Error(t, err)
			})
		}
	}
}
