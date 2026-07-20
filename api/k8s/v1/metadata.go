package v1

const (
	PodModelLabel = "model"
	// PodHashLabel is a label key used to store the hash of the Pod spec
	// that was used to create the Pod. This is used to determine if a Pod
	// needs to be recreated.
	PodHashLabel            = "pod-hash"
	LeaderHashLabel         = "leader-hash"
	WorkerHashLabel         = "worker-hash"
	ModelFeatureLabelDomain = "features.kubeai.org"

	// ModelPodIPAnnotation is the annotation key used to specify an IP
	// to use for the model Pod instead of the IP address in the status of the Pod.
	// Use in conjunction with --allow-pod-address-override for development purposes.
	ModelPodIPAnnotation   = "model-pod-ip"
	ModelPodPortAnnotation = "model-pod-port"

	ModelCacheEvictionFinalizer = "kubeai.org/cache-eviction"
)

func PVCModelAnnotation(modelName string) string {
	return "models.kubeai.org/" + modelName
}

const (
	PodAdapterLabelPrefix = "adapter.kubeai.org/"
)

func PodAdapterLabel(adapterID string) string {
	return PodAdapterLabelPrefix + adapterID
}
