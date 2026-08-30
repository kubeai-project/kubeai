# Load Models from OCI Images

You can package your models into OCI images or [CNCF ModelPack](https://github.com/modelpack/model-spec)
artifacts and let KubeAI use them for serving.

KubeAI pulls the reference through a running [`llmman serve`](https://github.com/llmmanorg/llmman)
daemon in an init container, into a volume the model server Pod reads. llmman
speaks both the registry v2 protocol and the ModelPack media types, so the same
URL works for an image and for an artifact, on any container runtime.

> **Note:** this used to mount a Kubernetes
> [image volume](https://kubernetes.io/docs/tasks/configure-pod-container/image-volumes/),
> which restricted OCI *artifacts* to CRI-O clusters (containerd can only mount
> runnable images) and required the `ImageVolume` feature gate. Neither
> restriction applies now.

## Requirements

An `llmman serve` daemon must be reachable from the model Pod. By default the
init container uses llmman's own default address, `127.0.0.1:17434`. To point
every model Pod at one shared daemon, set it in your Helm values:

```yaml
modelLoading:
  llmmanHost: "llmman.kubeai.svc:17434"
```

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

Registry credentials are configured on the `llmman serve` daemon rather than on
each Model, so one place covers every model pulled through it. See llmman's
`llmman login` documentation.

The previous image pull `Secret` mechanism no longer applies, since KubeAI is
not asking the kubelet to pull the reference.

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
