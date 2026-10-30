# Whisper speech-to-text with vLLM

This example serves `openai/whisper-large-v3-turbo` through KubeAI's
`POST /openai/v1/audio/transcriptions` endpoint using vLLM v0.31.0 with its
audio dependencies. It provides an alternative to the archived
`fedirz/faster-whisper-server` image discussed in
[issue #561](https://github.com/kubeai-project/kubeai/issues/561).

The example uses one NVIDIA L4 GPU and the existing `nvidia-gpu-l4` resource
profile. Adapt that profile to your cluster. The base image uses CUDA 13.0;
the GPU node's NVIDIA driver must support it. KubeAI and its Model CRD must
already be installed in the namespace where you deploy this Model.

From the repository root, build an image with the audio extras and push it to
a registry accessible to the GPU nodes:

```bash
export IMAGE=registry.example.com/kubeai/vllm-whisper:v0.31.0
docker build -t "$IMAGE" examples/vllm-speech-to-text
docker push "$IMAGE"
```

Replace `spec.image` in `model.yaml` with that image reference, then deploy:

```bash
kubectl apply -f examples/vllm-speech-to-text/model.yaml
```

Alternatively, replace `catalog.whisper-large-v3-turbo-vllm.image` in
`models-helm-values.yaml` and install it through the models chart:

```bash
helm upgrade --install kubeai-models ./charts/models \
  -f examples/vllm-speech-to-text/models-helm-values.yaml
```

Use the same namespace as your KubeAI installation (`-n <namespace>` for
both kubectl and Helm). Use one deployment method for this Model.

Watch the Model's pod until it is ready, then expose KubeAI locally:

```bash
kubectl get pods -l model=whisper-large-v3-turbo-vllm -w
# In another terminal; use your KubeAI service name if it differs.
kubectl port-forward service/kubeai 8000:80
```

In another terminal, transcribe a local WAV file:

```bash
curl --fail-with-body http://localhost:8000/openai/v1/audio/transcriptions \
  -F 'model=whisper-large-v3-turbo-vllm' \
  -F 'file=@audio.wav' \
  -F 'language=en' \
  -F 'response_format=json'
```

The model name in requests is the KubeAI Model name. vLLM downloads the native
Hugging Face Whisper checkpoint when its pod starts; this example does not use
the CTranslate2 checkpoints from the FasterWhisper catalog. The upload limit
is 25 MB by default. The expected response has a `text` field.

See the [speech-to-text guide](../../docs/how-to/configure-speech-to-text.md)
for Python SDK usage and migration notes, and
[vLLM's speech API documentation](https://docs.vllm.ai/en/v0.31.0/serving/online_serving/speech_to_text/)
for supported parameters.
