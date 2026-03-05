# Configure resource profiles

This guide will cover modifying preconfigured [resource profiles](../concepts/resource-profiles.md) and adding your own.

## Modifying preconfigured resource profiles

The KubeAI helm chart comes with preconfigured resource profiles for common resource types such as NVIDIA L4 GPUs. You can view these profiles in the [default helm values file](https://github.com/kubeai-project/kubeai/blob/main/charts/kubeai/values.yaml).

These profiles usually require some additional settings based on the cluster/cloud that KubeAI is installed into. You can modify a resource profile by setting custom helm values and runing `helm install` or `helm upgrade`. For example, if you are installing KubeAI on GKE you will need to set GKE-specific node selectors:

```yaml
# helm-values.yaml
resourceProfiles:
  nvidia-gpu-l4:
    nodeSelector:
      cloud.google.com/gke-accelerator: "nvidia-l4"
      cloud.google.com/gke-spot: "true"
```

NOTE: See the cloud-specific installation guide for a comprehensive list of settings.

## Adding additional resource profiles

If the preconfigured resource profiles do not meet your needs you can add additional profiles by appending to the `.resourceProfiles` object in the helm values file you use to install KubeAI.

```yaml
# helm-values.yaml
resourceProfiles:
  my-custom-gpu:
    imageName: "optional-custom-image-name"
    nodeSelector:
      my-custom-node-pool: "some-value"
    limits:
      custom.com/gpu: "1"
    requests:
      custom.com/gpu: "1"
      cpu: "3"
      memory: "12Gi"
    schedulerName: "my-custom-scheduler"
    runtimeClassName: "my-custom-runtime-class"
```

If you need to run custom model server images on your resource profile, make sure to also add those in the `modelServers` section:

```yaml
# helm-values.yaml
modelServers:
  VLLM:
    images:
      optional-custom-image-name: "my-repo/my-vllm-image:v1.2.3"
  OLlama:
    images:
      optional-custom-image-name: "my-repo/my-ollama-image:v1.2.3"
```

# Next

- See the guide on [how to install models](./install-models.md) which includes how to configure the resource profile to use for a given model.
- For models that span multiple nodes, see [how to serve multi-node models](./serve-multi-node-models.md).