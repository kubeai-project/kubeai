package config

import (
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/go-playground/validator/v10"
	corev1 "k8s.io/api/core/v1"
)

type System struct {
	SecretNames SecretNames `json:"secretNames" validate:"required"`

	ModelServers ModelServers `json:"modelServers" validate:"required"`

	ModelLoading ModelLoading `json:"modelLoading" validate:"required"`

	ResourceProfiles map[string]ResourceProfile `json:"resourceProfiles" validate:"required"`

	CacheProfiles map[string]CacheProfile `json:"cacheProfiles"`

	Messaging Messaging `json:"messaging"`

	// MetricsAddr is the address the metric endpoint binds to.
	// Defaults to ":8080"
	MetricsAddr string `json:"metricsAddr" validate:"required"`

	// HealthAddr is the address the health probe endpoint binds to.
	// Defaults to ":8081"
	HealthAddress string `json:"healthAddress" validate:"required"`

	ModelAutoscaling ModelAutoscaling `json:"modelAutoscaling" validate:"required"`

	ModelServerPods ModelServerPods `json:"modelServerPods,omitempty"`

	ModelRollouts ModelRollouts `json:"modelRollouts"`

	LeaderElection LeaderElection `json:"leaderElection"`

	// AllowPodAddressOverride will allow the pod address to be overridden by the Model objects. Useful for development purposes.
	AllowPodAddressOverride bool `json:"allowPodAddressOverride"`

	// FixedSelfMetricAddrs is a list of fixed addresses to be used when scraping metrics for autoscaling. Useful for development purposes.
	FixedSelfMetricAddrs []string `json:"fixedSelfMetricAddrs,omitempty"`
}

func (s *System) DefaultAndValidate() error {
	if s.MetricsAddr == "" {
		s.MetricsAddr = ":8080"
	}
	if s.HealthAddress == "" {
		s.HealthAddress = ":8081"
	}

	for i := range s.Messaging.Streams {
		if s.Messaging.Streams[i].MaxHandlers == 0 {
			s.Messaging.Streams[i].MaxHandlers = 1
		}
	}

	if s.ModelAutoscaling.Interval.Duration == 0 {
		s.ModelAutoscaling.Interval.Duration = 10 * time.Second
	}
	if s.ModelAutoscaling.TimeWindow.Duration == 0 {
		s.ModelAutoscaling.TimeWindow.Duration = 10 * time.Minute
	}

	if s.LeaderElection.LeaseDuration.Duration == 0 {
		s.LeaderElection.LeaseDuration.Duration = 15 * time.Second
	}
	if s.LeaderElection.RenewDeadline.Duration == 0 {
		s.LeaderElection.RenewDeadline.Duration = 10 * time.Second
	}
	if s.LeaderElection.RetryPeriod.Duration == 0 {
		s.LeaderElection.RetryPeriod.Duration = 2 * time.Second
	}

	if s.CacheProfiles == nil {
		s.CacheProfiles = map[string]CacheProfile{}
	}

	return validator.New(validator.WithRequiredStructEnabled()).Struct(s)
}

type LeaderElection struct {
	// LeaseDuration is the duration that non-leader candidates will
	// wait to force acquire leadership. This is measured against time of
	// last observed ack.
	//
	// A client needs to wait a full LeaseDuration without observing a change to
	// the record before it can attempt to take over. When all clients are
	// shutdown and a new set of clients are started with different names against
	// the same leader record, they must wait the full LeaseDuration before
	// attempting to acquire the lease. Thus LeaseDuration should be as short as
	// possible (within your tolerance for clock skew rate) to avoid a possible
	// long waits in the scenario.
	//
	// Defaults to 15 seconds.
	LeaseDuration Duration `json:"leaseDuration"`
	// RenewDeadline is the duration that the acting master will retry
	// refreshing leadership before giving up.
	//
	// Defaults to 10 seconds.
	RenewDeadline Duration `json:"renewDeadline"`
	// RetryPeriod is the duration the LeaderElector clients should wait
	// between tries of actions.
	//
	// Defaults to 2 seconds.
	RetryPeriod Duration `json:"retryPeriod"`
}

type ModelRollouts struct {
	// Surge is the number of additional Pods to create when rolling out an update.
	Surge int32 `json:"surge"`
}

type ModelAutoscaling struct {
	// Interval is the time between each autoscaling check.
	// Defaults to 10 seconds.
	Interval Duration `json:"interval" validate:"required"`
	// TimeWindow that the autoscaling algorithm will consider when
	// calculating the average number of requests.
	// Defaults to 10 minutes.
	TimeWindow Duration `json:"timeWindow" validate:"required"`
	// StateConfigMapName is the name of the ConfigMap that will be used
	// to store the state of the autoscaler. This ConfigMap ensures that
	// the autoscaler can recover from crashes and restarts without losing
	// its state.
	// Required.
	StateConfigMapName string `json:"stateConfigMapName" validate:"required"`
}

// RequiredConsecutiveScaleDowns returns the number of consecutive scale down
// operations required before the deployment is scaled down. This is calculated
// by dividing the ScaleDownDelay by the Interval.
func (a *ModelAutoscaling) RequiredConsecutiveScaleDowns(scaleDownDelaySeconds int64) int {
	return int(math.Ceil(float64(time.Duration(scaleDownDelaySeconds)*time.Second) / float64(a.Interval.Duration)))
}

// AverageWindowCount returns the number of intervals that will be considered when
// calculating the average value.
func (a *ModelAutoscaling) AverageWindowCount() int {
	return int(math.Ceil(float64(a.TimeWindow.Duration) / float64(a.Interval.Duration)))
}

type SecretNames struct {
	Alibaba     string `json:"alibaba" required:"true"`
	AWS         string `json:"aws" required:"true"`
	GCP         string `json:"gcp" required:"true"`
	Huggingface string `json:"huggingface" required:"true"`
}

type Messaging struct {
	// ErrorMaxBackoff is the maximum backoff time that will be applied when
	// consecutive errors are encountered.
	ErrorMaxBackoff Duration        `json:"errorMaxBackoff"`
	Streams         []MessageStream `json:"streams"`
}

type Duration struct {
	time.Duration
}

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch value := v.(type) {
	case float64:
		d.Duration = time.Duration(value)
		return nil
	case string:
		var err error
		d.Duration, err = time.ParseDuration(value)
		if err != nil {
			return err
		}
		return nil
	default:
		return errors.New("invalid duration")
	}
}

type ResourceProfile struct {
	ImageName        string              `json:"imageName"`
	Requests         corev1.ResourceList `json:"requests,omitempty"`
	Limits           corev1.ResourceList `json:"limits,omitempty"`
	NodeSelector     map[string]string   `json:"nodeSelector,omitempty"`
	Affinity         *corev1.Affinity    `json:"affinity,omitempty"`
	Tolerations      []corev1.Toleration `json:"tolerations,omitempty"`
	SchedulerName    string              `json:"schedulerName,omitempty"`
	RuntimeClassName *string             `json:"runtimeClassName,omitempty"`
}

type CacheProfile struct {
	SharedFilesystem *CacheSharedFilesystem `json:"sharedFilesystem,omitempty"`
}

type CacheSharedFilesystem struct {
	// StorageClassName is the name of the StorageClass to use for the shared filesystem.
	StorageClassName string `json:"storageClassName,omitempty" validate:"required_without=PersistentVolumeName"`
	// PersistentVolumeName is the name of the PersistentVolume to use for the shared filesystem.
	// This is usually used if you have an existing filesystem that you want to use.
	PersistentVolumeName string `json:"persistentVolumeName,omitempty" validate:"required_without=StorageClassName"`
}

type MessageStream struct {
	RequestsURL  string `json:"requestsURL"`
	ResponsesURL string `json:"responsesURL"`
	// MaxHandlers is the maximum number of handlers that will be started for this stream.
	// Must be greater than 0. Defaults to 1.
	MaxHandlers int `json:"maxHandlers" validate:"min=1"`
}

type ModelServers struct {
	OLlama        ModelServer `json:"OLlama"`
	VLLM          ModelServer `json:"VLLM"`
	FasterWhisper ModelServer `json:"FasterWhisper"`
	Infinity      ModelServer `json:"Infinity"`
}

type ModelServer struct {
	Images map[string]string `json:"images"`
}

type ModelLoading struct {
	Image string `json:"image" validate:"required"`
}

type JSONPatch struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value"`
}

type ModelServerPods struct {
	// The service account to use for all model pods
	ModelServiceAccountName string `json:"serviceAccountName,omitempty"`

	// Security Context for the model pods
	ModelPodSecurityContext *corev1.PodSecurityContext `json:"podSecurityContext,omitempty"`

	// Security Context for the model pod containers
	ModelContainerSecurityContext *corev1.SecurityContext `json:"securityContext,omitempty"`

	// ImagePullSecrets is a list of references to secrets in the same namespace to use for pulling any of the images
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// JSONPatches is a list of patches to apply to the model pod template.
	// This is a JSON Patch as defined in RFC 6902.
	// https://datatracker.ietf.org/doc/html/rfc6902
	JSONPatches []JSONPatch `json:"jsonPatches,omitempty"`
}
