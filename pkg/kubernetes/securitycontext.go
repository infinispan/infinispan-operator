package kubernetes

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/utils/ptr"
)

// DefaultPodSecurityContext returns the pod-level securityContext defaults applied to all
// operator-provisioned pods.
func DefaultPodSecurityContext() *corev1.PodSecurityContext {
	return &corev1.PodSecurityContext{
		RunAsNonRoot: ptr.To(true),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
	}
}

// DefaultContainerSecurityContext returns the container-level securityContext defaults
// applied to all operator-provisioned containers.
func DefaultContainerSecurityContext() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr.To(false),
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
		RunAsNonRoot: ptr.To(true),
	}
}

// MergePodSecurityContext returns the hardened pod-level defaults with the user-supplied override
// deep-merged on top (override wins). A nil override yields the pure defaults.
func MergePodSecurityContext(override *corev1.PodSecurityContext) (*corev1.PodSecurityContext, error) {
	defaults := DefaultPodSecurityContext()
	if override == nil {
		return defaults, nil
	}
	merged := &corev1.PodSecurityContext{}
	if err := mergeSecurityContext(defaults, override, merged); err != nil {
		return nil, err
	}
	return merged, nil
}

// MergeContainerSecurityContext returns the hardened container-level defaults with the user-supplied
// override deep-merged on top (override wins). A nil override yields the pure defaults.
func MergeContainerSecurityContext(override *corev1.SecurityContext) (*corev1.SecurityContext, error) {
	defaults := DefaultContainerSecurityContext()
	if override == nil {
		return defaults, nil
	}
	merged := &corev1.SecurityContext{}
	if err := mergeSecurityContext(defaults, override, merged); err != nil {
		return nil, err
	}
	return merged, nil
}

// mergeSecurityContext applies a strategic merge patch of override over defaults, unmarshalling the
// result into out. Strategic merge respects pointer/omitempty semantics so unset user fields inherit
// the defaults while set fields (including explicit false booleans) override them. Lists without a
// merge key (e.g. capabilities.drop) are replaced by the user-supplied value.
func mergeSecurityContext(defaults, override, out interface{}) error {
	base, err := json.Marshal(defaults)
	if err != nil {
		return fmt.Errorf("unable to marshal default securityContext: %w", err)
	}
	patch, err := json.Marshal(override)
	if err != nil {
		return fmt.Errorf("unable to marshal securityContext override: %w", err)
	}
	merged, err := strategicpatch.StrategicMergePatch(base, patch, out)
	if err != nil {
		return fmt.Errorf("unable to merge securityContext: %w", err)
	}
	return json.Unmarshal(merged, out)
}
