# Configure speech-to-text

KubeAI exposes `POST /openai/v1/audio/transcriptions` for Models with
`features: [SpeechToText]`. You can serve native Whisper checkpoints with
vLLM as an alternative to the archived
[fedirz/faster-whisper-server](https://github.com/fedirz/faster-whisper-server)
backend reported in [issue #561](https://github.com/kubeai-project/kubeai/issues/561).

## Whisper with vLLM

The [complete example](https://github.com/kubeai-project/kubeai/tree/main/examples/vllm-speech-to-text)
includes a Dockerfile, a Model manifest, and values for the models Helm chart.
It uses `openai/whisper-large-v3-turbo`, vLLM v0.31.0, and one NVIDIA L4 GPU.
KubeAI and the Model CRD must already be installed. Deploy the Model in the
same namespace as KubeAI, and adapt the resource profile to your GPU nodes.
The image uses CUDA 13.0; your NVIDIA driver must support that runtime.

### Build the audio image

vLLM requires its optional audio dependencies for transcription. The example
Dockerfile adds `vllm[audio]` to the pinned upstream image and preserves the
installed CUDA stack through package constraints.

Run these commands from the repository root, replacing the registry with one
your cluster can pull from:

```bash
export IMAGE=registry.example.com/kubeai/vllm-whisper:v0.31.0
docker build -t "$IMAGE" examples/vllm-speech-to-text
docker push "$IMAGE"
```

### Create a Model

Replace the image reference with the image you published, then apply this
manifest in your KubeAI namespace:

```yaml
apiVersion: kubeai.org/v1
kind: Model
metadata:
  name: whisper-large-v3-turbo-vllm
spec:
  features: [SpeechToText]
  owner: openai
  url: hf://openai/whisper-large-v3-turbo
  engine: VLLM
  image: registry.example.com/kubeai/vllm-whisper:v0.31.0
  resourceProfile: nvidia-gpu-l4:1
  minReplicas: 1
  args:
    - --max-model-len=448
    - --max-num-seqs=16
    - --gpu-memory-utilization=0.5
    - --enforce-eager
```

The manifest is also available at `examples/vllm-speech-to-text/model.yaml`:

```bash
kubectl apply -f examples/vllm-speech-to-text/model.yaml
```

`minReplicas: 1` keeps the model loaded for this example. Setting it to `0`
allows KubeAI to scale to zero; the first request then waits for the model to
download and start. vLLM loads the Hugging Face checkpoint at startup, so a
separate model-loading API call is unnecessary.

### Install through Helm

As an alternative to applying the Model directly, replace the image reference
in `examples/vllm-speech-to-text/models-helm-values.yaml` and run:

```bash
helm upgrade --install kubeai-models ./charts/models \
  -f examples/vllm-speech-to-text/models-helm-values.yaml
```

Use the same namespace as KubeAI (`-n <namespace>` for Helm and kubectl).

## Transcribe audio

Wait for the Model's pod to become ready:

```bash
kubectl get pods -l model=whisper-large-v3-turbo-vllm -w
```

Expose KubeAI locally in another terminal (adjust the service name if needed):

```bash
kubectl port-forward service/kubeai 8000:80
```

Use the KubeAI Model name in the multipart `model` field. For a local WAV file:

```bash
curl --fail-with-body http://localhost:8000/openai/v1/audio/transcriptions \
  -F 'file=@audio.wav' \
  -F 'language=en' \
  -F 'response_format=json' \
  -F 'model=whisper-large-v3-turbo-vllm'
```

The response contains the transcript in its `text` field. You can also use the
OpenAI Python SDK with KubeAI's public base URL:

```python
from openai import OpenAI

client = OpenAI(api_key="ignored", base_url="http://localhost:8000/openai/v1")
with open("audio.wav", "rb") as audio:
    transcript = client.audio.transcriptions.create(
        model="whisper-large-v3-turbo-vllm",
        file=audio,
        language="en",
        response_format="json",
    )
print(transcript.text)
```

vLLM's default upload limit is 25 MB. The `language` field describes the input
audio; omit it for automatic detection. Supported parameters and formats depend
on the backend version; see
[vLLM's speech API documentation](https://docs.vllm.ai/en/v0.31.0/serving/online_serving/speech_to_text/).

## Existing FasterWhisper deployments

The legacy CPU catalog entry remains available for existing installations:

```yaml
catalog:
  faster-whisper-medium-en-cpu:
    enabled: true
    minReplicas: 1
```

That entry uses `engine: FasterWhisper` and
`hf://Systran/faster-whisper-medium.en`, a CTranslate2 checkpoint. The vLLM
example uses a native Hugging Face Whisper checkpoint and a GPU resource
profile, so migrating requires changing the engine, checkpoint URL, image,
and resource profile together. Update clients to use the new Model name.

Changing only the FasterWhisper image to a Speaches image is insufficient:
Speaches has its own startup, model-loading, and user configuration. The vLLM
example provides a separate supported engine configuration for transcription.
