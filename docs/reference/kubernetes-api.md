# Kubernetes API

## Packages
- [kubeai.org/v1](#kubeaiorgv1)


## kubeai.org/v1

Package v1 contains API Schema definitions for the kubeai v1 API group

### Resource Types
- [Model](#model)



#### Model



Model resources define the ML models that will be served by KubeAI.





| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `apiVersion` _string_ | `kubeai.org/v1` | | |
| `kind` _string_ | `Model` | | |
| `metadata` _[ObjectMeta](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.3/#objectmeta-v1-meta)_ | Refer to Kubernetes API documentation for fields of `metadata`. |  |  |
| `spec` _[ModelSpec](#modelspec)_ |  |  |  |
| `status` _[ModelStatus](#modelstatus)_ |  |  |  |


#### ModelFeature

_Underlying type:_ _string_



_Validation:_
- Enum: [TextGeneration TextEmbedding SpeechToText]

_Appears in:_
- [ModelSpec](#modelspec)



#### ModelSpec



ModelSpec defines the desired state of Model.



_Appears in:_
- [Model](#model)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `url` _string_ | URL of the model to be served.<br />Currently only the following formats are supported:<br />For VLLM & FasterWhisper engines: "hf://<model-repo>/<model-name>"<br />For OLlama engine: "ollama://<model> |  |  |
| `features` _[ModelFeature](#modelfeature) array_ | Features that the model supports.<br />Dictates the APIs that are available for the model. |  | Enum: [TextGeneration TextEmbedding SpeechToText] <br /> |
| `engine` _string_ | Engine to be used for the server process. |  | Enum: [OLlama VLLM FasterWhisper] <br /> |
| `resourceProfile` _string_ | ResourceProfile required to serve the model.<br />Use the format "<resource-profile-name>:<count>".<br />Example: "nvidia-gpu-l4:2" - 2x NVIDIA L4 GPUs.<br />Must be a valid ResourceProfile defined in the system config. |  |  |
| `image` _string_ | Image to be used for the server process.<br />Will be set from ResourceProfile + Engine if not specified. |  |  |
| `args` _string array_ | Args to be added to the server process. |  |  |
| `env` _object (keys:string, values:string)_ | Env variables to be added to the server process. |  |  |
| `replicas` _integer_ | Replicas is the number of Pod replicas that should be actively<br />serving the model. KubeAI will manage this field unless AutoscalingDisabled<br />is set to true. |  |  |
| `minReplicas` _integer_ | MinReplicas is the minimum number of Pod replicas that the model can scale down to.<br />Note: 0 is a valid value. |  | Minimum: 0 <br />Optional: \{\} <br /> |
| `maxReplicas` _integer_ | MaxReplicas is the maximum number of Pod replicas that the model can scale up to.<br />Empty value means no limit. |  | Minimum: 1 <br /> |
| `autoscalingDisabled` _boolean_ | AutoscalingDisabled will stop the controller from managing the replicas<br />for the Model. When disabled, metrics will not be collected on server Pods. |  |  |
| `targetRequests` _integer_ | TargetRequests is average number of active requests that the autoscaler<br />will try to maintain on model server Pods. | 100 | Minimum: 1 <br /> |
| `scaleDownDelaySeconds` _integer_ | ScaleDownDelay is the minimum time before a deployment is scaled down after<br />the autoscaling algorithm determines that it should be scaled down. | 30 |  |
| `owner` _string_ | Owner of the model. Used solely to populate the owner field in the<br />OpenAI /v1/models endpoint.<br />DEPRECATED. |  | Optional: \{\} <br /> |


#### ModelStatus



ModelStatus defines the observed state of Model.



_Appears in:_
- [Model](#model)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `replicas` _[ModelStatusReplicas](#modelstatusreplicas)_ |  |  |  |


#### ModelStatusReplicas







_Appears in:_
- [ModelStatus](#modelstatus)

| Field | Description | Default | Validation |
| --- | --- | --- | --- |
| `all` _integer_ |  |  |  |
| `ready` _integer_ |  |  |  |


