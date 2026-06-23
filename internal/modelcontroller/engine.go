package modelcontroller

import (
	"fmt"

	kubeaiv1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/kubeai-project/kubeai/internal/config"
	corev1 "k8s.io/api/core/v1"
)

// EngineConfig holds the configuration that engine implementations need to build pods.
// It is intentionally narrow — engines should not depend on the full ModelReconciler.
type EngineConfig struct {
	AllowPodAddressOverride bool
	ModelServerPods         config.ModelServerPods
	ModelLoaders            config.ModelLoading
	SecretNames             config.SecretNames
}

type Engine interface {
	PodForModel(m *kubeaiv1.Model, c ModelConfig) *corev1.Pod
}

type EngineRegistry struct {
	engines map[string]Engine
}

func NewEngineRegistry(cfg EngineConfig) *EngineRegistry {
	return &EngineRegistry{
		engines: map[string]Engine{
			kubeaiv1.VLLMEngine:          &VLLMEngine{cfg: cfg},
			kubeaiv1.OLlamaEngine:        &OLlamaEngine{cfg: cfg},
			kubeaiv1.FasterWhisperEngine: &FasterWhisperEngine{cfg: cfg},
			kubeaiv1.InfinityEngine:      &InfinityEngine{cfg: cfg},
		},
	}
}

func (reg *EngineRegistry) Get(name string) (Engine, error) {
	if e, ok := reg.engines[name]; ok {
		return e, nil
	}
	return nil, fmt.Errorf("unknown engine: %q", name)
}
