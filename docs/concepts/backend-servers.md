# Backend Servers

KubeAI serves ML models by launching Pods on Kubernetes. The configuration and lifecycle of these Pods are managed by the KubeAI controller. Every model server Pod loads exactly one model on startup.

In a Model manifest you can define what server to use for inference (`VLLM`, `SGLang`, `LlamaCpp`, `OLlama`, `FasterWhisper`, `Infinity`). Any model-specific settings can be passed to the server process via the `args` and `env` fields.

## Next

Read about [how to install models](../how-to/install-models.md).