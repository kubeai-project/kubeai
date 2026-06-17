package modelcontroller

import (
	"fmt"
	"strconv"
	"strings"

	kubeaiv1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/kubeai-project/kubeai/internal/config"
	corev1 "k8s.io/api/core/v1"
)

// enginePodBuilder constructs the base Pod spec for a given engine.
// Each engine file (engine_vllm.go, engine_ollama.go, etc.) provides a
// builder function matching this signature.
type enginePodBuilder func(m *kubeaiv1.Model, c ModelConfig) *corev1.Pod

// ModelConfig holds the resolved configuration for a Model, combining the
// resource profile, cache profile, image, source, and optional multi-node config.
type ModelConfig struct {
	config.CacheProfile
	config.ResourceProfile
	Image      string
	Source     modelSource
	LWSConfig  *LWSConfig
	PodBuilder enginePodBuilder `json:"-"`
}

// LWSConfig holds multi-node group configuration for LeaderWorkerSet.
type LWSConfig struct {
	TensorParallelSize   int
	PipelineParallelSize int // Also used as LWS group size
}

// ResourceProfileRef is the parsed representation of the resource profile string.
// It separates the profile name, GPU multiplier, and optional multi-node config.
type ResourceProfileRef struct {
	Name      string
	Count     int        // GPU/resource multiplier (first numeric segment)
	LWSConfig *LWSConfig // nil for single-node profiles
}

// parseResourceProfile parses a resource profile string in the format
// "<name>:<count>" (single-node) or "<name>:<tp>:<pp>" (multi-node).
// For multi-node profiles, Count is the tensor-parallel size and
// LWSConfig.PipelineParallelSize is the pipeline-parallel / group size.
func parseResourceProfile(s string) (ResourceProfileRef, error) {
	split := strings.Split(s, ":")
	if len(split) != 2 && len(split) != 3 {
		return ResourceProfileRef{}, fmt.Errorf(
			"invalid resource profile: %q, expected <name>:<count> or <name>:<tp>:<pp>",
			s,
		)
	}

	name := split[0]
	count, err := strconv.Atoi(split[1])
	if err != nil {
		return ResourceProfileRef{}, fmt.Errorf("invalid count in resource profile %q: %w", split[1], err)
	}

	ref := ResourceProfileRef{Name: name, Count: count}

	if len(split) == 3 {
		ppSize, err := strconv.Atoi(split[2])
		if err != nil {
			return ResourceProfileRef{}, fmt.Errorf("invalid pipeline-parallel size in resource profile %q: %w", split[2], err)
		}
		ref.LWSConfig = &LWSConfig{
			TensorParallelSize:   count,
			PipelineParallelSize: ppSize,
		}
	}

	return ref, nil
}

// getModelConfig resolves the full ModelConfig for a given Model, looking up
// the resource profile, cache profile, image, source, and engine pod builder.
func (r *ModelReconciler) getModelConfig(model *kubeaiv1.Model) (ModelConfig, error) {
	var result ModelConfig

	src, err := r.parseModelSource(model.Spec.URL)
	if err != nil {
		return result, fmt.Errorf("parsing model source: %w", err)
	}
	result.Source = src

	if model.Spec.CacheProfile != "" {
		cacheProfile, ok := r.CacheProfiles[model.Spec.CacheProfile]
		if !ok {
			return result, fmt.Errorf("cache profile not found: %q", model.Spec.CacheProfile)
		}
		result.CacheProfile = cacheProfile
	}

	profileRef, err := parseResourceProfile(model.Spec.ResourceProfile)
	if err != nil {
		return result, err
	}

	if profileRef.LWSConfig != nil {
		if !r.DistributedInference {
			return result, fmt.Errorf("distributed inference is disabled: set distributedInference=true")
		}
		if model.Spec.Engine != kubeaiv1.VLLMEngine {
			return result, fmt.Errorf("multi-node (LWS) resource profiles only supported with VLLM engine, got %q", model.Spec.Engine)
		}
		result.LWSConfig = profileRef.LWSConfig
	}

	profile, ok := r.ResourceProfiles[profileRef.Name]
	if !ok {
		return result, fmt.Errorf("resource profile not found: %q", profileRef.Name)
	}

	requests := make(corev1.ResourceList)
	for key, quantity := range profile.Requests {
		q := quantity.DeepCopy()
		q.Mul(int64(profileRef.Count))
		requests[key] = q
	}

	limits := make(corev1.ResourceList)
	for key, quantity := range profile.Limits {
		q := quantity.DeepCopy()
		q.Mul(int64(profileRef.Count))
		limits[key] = q
	}

	result.ResourceProfile = profile
	result.Requests = requests
	result.Limits = limits

	if model.Spec.EnvFrom != nil {
		result.Source.modelSourcePodAdditions.envFrom = model.Spec.EnvFrom
	}

	image, err := r.lookupServerImage(model, profile)
	if err != nil {
		return result, fmt.Errorf("looking up server image: %w", err)
	}
	result.Image = image

	// Resolve engine pod builder.
	result.PodBuilder = r.resolveEnginePodBuilder(model.Spec.Engine)

	return result, nil
}

// resolveEnginePodBuilder returns the appropriate pod-building function for the engine.
func (r *ModelReconciler) resolveEnginePodBuilder(engine string) enginePodBuilder {
	switch engine {
	case kubeaiv1.OLlamaEngine:
		return r.oLlamaPodForModel
	case kubeaiv1.FasterWhisperEngine:
		return r.fasterWhisperPodForModel
	case kubeaiv1.InfinityEngine:
		return r.infinityPodForModel
	default:
		return r.vLLMPodForModel
	}
}

func (r *ModelReconciler) lookupServerImage(model *kubeaiv1.Model, profile config.ResourceProfile) (string, error) {
	if model.Spec.Image != "" {
		return model.Spec.Image, nil
	}

	var serverImgs map[string]string
	switch model.Spec.Engine {
	case kubeaiv1.OLlamaEngine:
		serverImgs = r.ModelServers.OLlama.Images
	case kubeaiv1.FasterWhisperEngine:
		serverImgs = r.ModelServers.FasterWhisper.Images
	case kubeaiv1.InfinityEngine:
		serverImgs = r.ModelServers.Infinity.Images
	default:
		serverImgs = r.ModelServers.VLLM.Images
	}

	// If no image name is provided for a profile, use the default image name.
	const defaultImageName = "default"
	imageName := defaultImageName
	if profile.ImageName != "" {
		imageName = profile.ImageName
	}

	if img, ok := serverImgs[imageName]; ok {
		return img, nil
	}

	// If the specific profile image name does not exist, use the default image name.
	if img, ok := serverImgs[defaultImageName]; ok {
		return img, nil
	} else {
		return "", fmt.Errorf("missing default server image")
	}
}
