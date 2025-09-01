/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Use "+NOTE: ..." comments to add notes to the code that wont show up in public API reference or Custom Resource Definition.

// ModelSpec defines the desired state of Model.
// +kubebuilder:validation:XValidation:rule="!has(self.cacheProfile) || self.url.startsWith(\"hf://\") || self.url.startsWith(\"s3://\") || self.url.startsWith(\"gs://\") || self.url.startsWith(\"oss://\")", message="cacheProfile is only supported with urls of format \"hf://...\", \"s3://...\", \"gs://...\", or \"oss://...\" at the moment."
// +kubebuilder:validation:XValidation:rule="!self.url.startsWith(\"gs://\") || has(self.cacheProfile)", message="urls of format \"gs://...\" only supported when using a cacheProfile"
// +kubebuilder:validation:XValidation:rule="!self.url.startsWith(\"oss://\") || has(self.cacheProfile)", message="urls of format \"oss://...\" only supported when using a cacheProfile"
// +kubebuilder:validation:XValidation:rule="!has(self.maxReplicas) || self.minReplicas <= self.maxReplicas", message="minReplicas should be less than or equal to maxReplicas."
// +kubebuilder:validation:XValidation:rule="!has(self.adapters) || self.engine == \"VLLM\"", message="adapters only supported with VLLM engine."
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.cacheProfile) || self.url == oldSelf.url", message="url is immutable when using cacheProfile."
// +NOTE: The self.files.all() check is considered "costly" by the Kubernetes API server and will be rejected if the number of files (and length of .path) are not restricted. These restrictions are applied in field-based validations below.
// +kubebuilder:validation:XValidation:rule="!has(self.files) || self.files.size() <= 1 || !self.files.exists(f, self.files.filter(other, other.path == f.path).size() > 1)", message="All file paths must be unique."
// +TODO: Limits on total file size should be less than limit of total ConfigMap (1MiB) data, this fails in version 1.29 (for exceeding "cost"): "!has(self.files) || self.files.map(f, size(f.content)).sum() <= 500000"
type ModelSpec struct {
	// URL of the model to be served.
	// Currently the following formats are supported:
	//
	// For VLLM, FasterWhisper, Infinity engines:
	//
	// "hf://<repo>/<model>"
	// "pvc://<pvcName>"
	// "pvc://<pvcName>/<pvcSubpath>"
	// "gs://<bucket>/<path>" (only with cacheProfile)
	// "oss://<bucket>/<path>" (only with cacheProfile)
	// "s3://<bucket>/<path>" (only with cacheProfile)
	//
	// For OLlama engine:
	//
	// "ollama://<model>"
	//
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self.startsWith(\"hf://\") || self.startsWith(\"pvc://\") || self.startsWith(\"ollama://\") || self.startsWith(\"s3://\") || self.startsWith(\"gs://\") || self.startsWith(\"oss://\")", message="url must start with \"hf://\", \"pvc://\", \"ollama://\", \"s3://\", \"gs://\", or \"oss://\" and not be empty."
	URL string `json:"url"`

	Adapters []Adapter `json:"adapters,omitempty"`

	// Features that the model supports.
	// Dictates the APIs that are available for the model.
	Features []ModelFeature `json:"features"`

	// Engine to be used for the server process.
	// +kubebuilder:validation:Enum=OLlama;VLLM;FasterWhisper;Infinity
	// +kubebuilder:validation:Required
	Engine string `json:"engine"`

	// ResourceProfile required to serve the model.
	// Use the format "<resource-profile-name>:<count>".
	// Example: "nvidia-gpu-l4:2" - 2x NVIDIA L4 GPUs.
	// Must be a valid ResourceProfile defined in the system config.
	ResourceProfile string `json:"resourceProfile,omitempty"`

	// CacheProfile to be used for caching model artifacts.
	// Must be a valid CacheProfile defined in the system config.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf", message="cacheProfile is immutable."
	CacheProfile string `json:"cacheProfile,omitempty"`

	// Image to be used for the server process.
	// Will be set from ResourceProfile + Engine if not specified.
	Image string `json:"image,omitempty"`

	// Args to be added to the server process.
	Args []string `json:"args,omitempty"`

	// Env variables to be added to the server process.
	Env map[string]string `json:"env,omitempty"`

	// Env variables to be added to the server process from Secret or ConfigMap.
	EnvFrom []corev1.EnvFromSource `json:"envFrom,omitempty"`

	// Replicas is the number of Pod replicas that should be actively
	// serving the model. KubeAI will manage this field unless AutoscalingDisabled
	// is set to true.
	Replicas *int32 `json:"replicas,omitempty"`

	// MinReplicas is the minimum number of Pod replicas that the model can scale down to.
	// Note: 0 is a valid value.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Optional
	MinReplicas int32 `json:"minReplicas"`

	// MaxReplicas is the maximum number of Pod replicas that the model can scale up to.
	// Empty value means no limit.
	// +kubebuilder:validation:Minimum=1
	MaxReplicas *int32 `json:"maxReplicas,omitempty"`

	// AutoscalingDisabled will stop the controller from managing the replicas
	// for the Model. When disabled, metrics will not be collected on server Pods.
	AutoscalingDisabled bool `json:"autoscalingDisabled,omitempty"`

	// TargetRequests is average number of active requests that the autoscaler
	// will try to maintain on model server Pods.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:default=100
	TargetRequests *int32 `json:"targetRequests"`

	// ScaleDownDelay is the minimum time before a deployment is scaled down after
	// the autoscaling algorithm determines that it should be scaled down.
	// +kubebuilder:default=30
	ScaleDownDelaySeconds *int64 `json:"scaleDownDelaySeconds"`

	// Owner of the model. Used solely to populate the owner field in the
	// OpenAI /v1/models endpoint.
	// DEPRECATED.
	// +kubebuilder:validation:Optional
	Owner string `json:"owner"`

	// LoadBalancing configuration for the model.
	// If not specified, a default is used based on the engine and request.
	// +kubebuilder:default={}
	LoadBalancing LoadBalancing `json:"loadBalancing,omitempty"`

	// Files to be mounted in the model Pods.
	// +kubebuilder:validation:MaxItems=10
	Files []File `json:"files,omitempty"`

	// PriorityClassName sets the priority class for all pods created for this model.
	// If specified, the PriorityClass must exist before the model is created.
	// This is useful for implementing priority and preemption for models.
	// +kubebuilder:validation:Optional
	PriorityClassName string `json:"priorityClassName,omitempty"`
}

// +kubebuilder:validation:Enum=TextGeneration;TextEmbedding;SpeechToText
type ModelFeature string

const (
	ModelFeatureTextGeneration = "TextGeneration"
	ModelFeatureTextEmbedding  = "TextEmbedding"
	// TODO (samos123): Add validation that Speech to Text only supports Faster Whisper.
	ModelFeatureSpeechToText = "SpeechToText"
)

const (
	OLlamaEngine        = "OLlama"
	VLLMEngine          = "VLLM"
	FasterWhisperEngine = "FasterWhisper"
	InfinityEngine      = "Infinity"
)

type Adapter struct {
	// Name must be a lowercase string with no spaces.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=^[a-z0-9-]+$
	// +kubebuilder:validation:MaxLength=63
	Name string `json:"name"`
	// +kubebuilder:validation:XValidation:rule="self.startsWith(\"hf://\") || self.startsWith(\"s3://\") || self.startsWith(\"gs://\") || self.startsWith(\"oss://\")", message="adapter url must start with \"hf://\", \"s3://\", \"gs://\", or \"oss://\"."
	URL string `json:"url"`
}

type LoadBalancing struct {
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=LeastLoad
	Strategy LoadBalancingStrategy `json:"strategy,omitempty"`
	// +kubebuilder:validation:Optional
	// +kubebuilder:default={}
	PrefixHash PrefixHash `json:"prefixHash,omitempty"`
	// +kubebuilder:validation:Optional
	// +kubebuilder:default={}
	RoutingKey RoutingKey `json:"routingKey,omitempty"`
}

// +kubebuilder:validation:Enum=LeastLoad;PrefixHash;RoutingKey
type LoadBalancingStrategy string

const (
	LeastLoadStrategy  LoadBalancingStrategy = "LeastLoad"
	PrefixHashStrategy LoadBalancingStrategy = "PrefixHash"
	RoutingKeyStrategy LoadBalancingStrategy = "RoutingKey"
)

type PrefixHash struct {
	// MeanLoadPercentage is the percentage that any given endpoint's load must not exceed
	// over the mean load of all endpoints in the hash ring. Defaults to 125% which is
	// a widely accepted value for the Consistent Hashing with Bounded Loads algorithm.
	// +kubebuilder:default=125
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=100
	MeanLoadPercentage int `json:"meanLoadFactor,omitempty"`
	// Replication is the number of replicas of each endpoint on the hash ring.
	// Higher values will result in a more even distribution of load but will
	// decrease lookup performance.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf", message="replication is immutable."
	// +kubebuilder:default=256
	// +kubebuilder:validation:Optional
	Replication int `json:"replication,omitempty"`
	// PrefixCharLength is the number of characters to count when building the prefix to hash.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=100
	PrefixCharLength int `json:"prefixCharLength,omitempty"`
}

type RoutingKey struct {
	// MeanLoadPercentage defines the maximum allowed load of any given endpoint
	// as a percentage over the mean load of all endpoints in the hash ring.
	// Defaults to 125%, which is a standard value for the Consistent Hashing with Bounded Loads algorithm.
	// +kubebuilder:default=125
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=100
	MeanLoadPercentage int `json:"meanLoadPercentage,omitempty"`
	// Replication defines the number of virtual replicas of each endpoint on the hash ring.
	// A higher value leads to better load distribution at the cost of slower lookup times.
	// This field is immutable after creation.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf", message="replication is immutable."
	// +kubebuilder:default=256
	// +kubebuilder:validation:Optional
	Replication int `json:"replication,omitempty"`
	// FallbackToLeastLoad controls behavior when the Routing-Key header is not present.
	// If true (default), the request is routed using the LeastLoad strategy.
	// If false, the request is rejected with a 400 error.
	// +kubebuilder:default=true
	// +kubebuilder:validation:Optional
	FallbackToLeastLoad bool `json:"fallbackToLeastLoad,omitempty"`
}

// File represents a file to be mounted in the model pod.
type File struct {
	// Path where the file should be mounted in the pod.
	// Must be an absolute path.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="self.startsWith('/') && !self.contains(':')", message="Path must be an absolute path, starting with /, and must not contain a ':' character."
	// +kubebuilder:validation:MaxLength=1024
	Path string `json:"path"`

	// Content of the file to be mounted.
	// Will be injected into a ConfigMap and mounted in the model Pods.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=100000
	Content string `json:"content"`
}

// ModelStatus defines the observed state of Model.
type ModelStatus struct {
	Replicas ModelStatusReplicas `json:"replicas,omitempty"`
	Cache    *ModelStatusCache   `json:"cache,omitempty"`
}

type ModelStatusReplicas struct {
	All   int32 `json:"all"`
	Ready int32 `json:"ready"`
}

type ModelStatusCache struct {
	Loaded bool `json:"loaded"`
}

// NOTE: Model name length should be limited to allow for the model name to be used in
// the names of the resources created by the controller.

// Model resources define the ML models that will be served by KubeAI.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:subresource:scale:specpath=.spec.replicas,statuspath=.status.replicas.all
// +kubebuilder:validation:XValidation:rule="size(self.metadata.name) <= 40", message="name must not exceed 40 characters."
type Model struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ModelSpec   `json:"spec,omitempty"`
	Status ModelStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ModelList contains a list of Models.
type ModelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Model `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Model{}, &ModelList{})
}
