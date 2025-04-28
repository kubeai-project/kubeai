package modelcontroller

import (
	"encoding/json"
	"fmt"

	v1 "github.com/kubeai-project/kubeai/api/k8s/v1"
	"github.com/kubeai-project/kubeai/internal/config"
	jsonpatch "gopkg.in/evanphx/json-patch.v4"
	corev1 "k8s.io/api/core/v1"
)

func applyJSONPatchToPod(patches []config.JSONPatch, pod *corev1.Pod) error {
	if len(patches) == 0 {
		return nil
	}

	pb, err := json.Marshal(patches)
	if err != nil {
		return fmt.Errorf("marshal pod patch: %w", err)
	}

	patch, err := jsonpatch.DecodePatch(pb)
	if err != nil {
		return fmt.Errorf("decode pod patch: %w", err)
	}

	podJson, err := json.Marshal(pod)
	if err != nil {
		return fmt.Errorf("marshal pod: %w", err)
	}

	patchedPodJson, err := patch.Apply(podJson)
	if err != nil {
		return fmt.Errorf("apply pod patch: %w", err)
	}

	patchedPod := &corev1.Pod{}
	if err := json.Unmarshal(patchedPodJson, patchedPod); err != nil {
		return fmt.Errorf("unmarshal patched pod: %w", err)
	}
	*pod = *patchedPod
	return nil
}

// convertJSONPatches converts from API version of JSONPatch to internal config version.
func convertJSONPatches(patches []v1.JSONPatch) []config.JSONPatch {
	if patches == nil {
		return nil
	}
	result := make([]config.JSONPatch, len(patches))
	for i, p := range patches {
		result[i] = config.JSONPatch{
			Op:    p.Op,
			Path:  p.Path,
			Value: p.Value,
		}
	}
	return result
}
