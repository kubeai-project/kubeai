# Load Models from OCI Images

You can package your models into OCI images and let KubeAI use them for serving.
KubeAI mounts the image contents directly into the model server Pod using a Kubernetes
[image volume](https://kubernetes.io/docs/tasks/configure-pod-container/image-volumes/),
so the model files are available without a separate download step.

> **Note:** The container runtime determines what kind of OCI references are supported:
> - When **containerd** is used as the container runtime, only **OCI images** are supported.
> - When **CRI-O** is used as the container runtime, both **OCI images** and **OCI artifacts** are supported.
>
> Image volumes also require a sufficiently recent Kubernetes version with the
> `ImageVolume` feature enabled on the cluster.

## vLLM

For vLLM, use the following URL format:
```yaml
url: oci://$REGISTRY/$REPOSITORY:$TAG    # Loads the model from the OCI image
```

For example:
```yaml
url: oci://docker.io/myorg/llama-3.1-8b:latest
```

The contents of the image are mounted at `/model` inside the model server Pod and vLLM is
configured to load the model from that path.

### Image requirements

The OCI image must contain the model files (the same layout vLLM expects when loading a model
from a local directory) at the root of the image filesystem. KubeAI mounts the image read-only,
so the model files must already be present in the image before creating the Model resource.

## Authentication for private registries

When pulling from a private registry, create a Kubernetes image pull `Secret` and configure
KubeAI to use it.

1. Create the pull secret:

   ```bash
   kubectl create secret docker-registry oci-pull-secret \
     --docker-server=$REGISTRY \
     --docker-username=$USERNAME \
     --docker-password=$PASSWORD
   ```

2. Reference the secret in your KubeAI installation:

   ```bash
   helm upgrade --install kubeai kubeai/kubeai \
       --set secrets.oci.name=oci-pull-secret \
       ...
   ```

   KubeAI adds this secret to the model server Pod's `imagePullSecrets` so the image volume can
   be pulled from the private registry.

**NOTE:** KubeAI does not automatically react to updates to credentials. You will need to
manually delete and allow KubeAI to recreate any failed Jobs/Pods that required credentials.

### Example: Loading OPT-125m from an OCI image

1. Push an OCI image that contains the model files to a registry your cluster can access.

2. Create a Model to load from the OCI image:

   ```yaml
   apiVersion: kubeai.org/v1
   kind: Model
   metadata:
     name: opt-125m-cpu
   spec:
     features: [TextGeneration]
     owner: facebook
     url: oci://docker.io/myorg/facebook-opt-125m:oci-image
     engine: VLLM
     resourceProfile: cpu:1
     minReplicas: 1
   ```

## Opt-in: pull through llmman

By default the kubelet mounts the reference as an image volume (see the note
above). To instead pull it through a running
[`llmman serve`](https://github.com/llmmanorg/llmman) daemon, append
`?via=llmman`:

```yaml
url: oci://$REGISTRY/$REPOSITORY:$TAG?via=llmman
```

An init container pulls the reference through the daemon and copies the files
into a volume mounted at `/model`. This works for
[CNCF ModelPack](https://github.com/modelpack/model-spec) artifacts (GGUF or
safetensors) on any container runtime, without the `ImageVolume` feature gate.
It does not handle runnable container images; keep the default for those.

Requirements:

- An `llmman serve` daemon reachable from the model Pods, using a
  `ReadWriteMany` PVC for its store: mount it and set `LLMMAN_MODELS` to
  `<mount>/store`.
- An init container image built from `components/llmman-loader`, the PVC, and
  optionally the daemon address, in your Helm values:

  ```yaml
  modelLoading:
    llmman: "$YOUR_REGISTRY/kubeai-llmman-loader:$TAG"
    llmmanStore: "llmman-store"
    llmmanHost: "llmman.kubeai.svc:17434" # default 127.0.0.1:17434
  ```

See `test/e2e/oci-model-llmman/llmman.yaml` for an example daemon.

Registry credentials for this path are configured on the daemon
(`llmman login`); `secrets.oci` does not apply.
