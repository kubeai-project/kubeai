package modelcontroller

import (
	"encoding/json"
	"fmt"

	v1 "github.com/substratusai/kubeai/api/k8s/v1"
	"github.com/substratusai/kubeai/internal/config"
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

func applyModelJSONPatchToPod(patches []v1.JSONPatch, pod *corev1.Pod) error {
	if len(patches) == 0 {
		return nil
	}

	// Convert v1.JSONPatch to interface{} with proper value/from handling
	configPatches := make([]interface{}, len(patches))
	for i, p := range patches {
		var value interface{}
		if p.Value != "" {
			if err := json.Unmarshal([]byte(p.Value), &value); err != nil {
				return fmt.Errorf("failed to decode JSON value for patch %d: %w", i, err)
			}
		}

		patch := map[string]interface{}{
			"op":   p.Op,
			"path": p.Path,
		}

		// Set value or from based on operation type
		switch p.Op {
		case "add", "replace", "test":
			patch["value"] = value
		case "move", "copy":
			patch["from"] = p.From
		case "remove":
			// No additional fields needed
		default:
			return fmt.Errorf("invalid operation type: %s", p.Op)
		}

		configPatches[i] = patch
	}

	pb, err := json.Marshal(configPatches)
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
