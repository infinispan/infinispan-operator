package kubernetes

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

func TestMergePodSecurityContext_NilReturnsDefaults(t *testing.T) {
	merged, err := MergePodSecurityContext(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if merged.RunAsNonRoot == nil || !*merged.RunAsNonRoot {
		t.Errorf("expected runAsNonRoot=true, got %v", merged.RunAsNonRoot)
	}
	if merged.SeccompProfile == nil || merged.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Errorf("expected seccompProfile RuntimeDefault, got %v", merged.SeccompProfile)
	}
	if merged.RunAsUser != nil {
		t.Errorf("expected no default runAsUser (OpenShift injects it), got %v", *merged.RunAsUser)
	}
}

func TestMergePodSecurityContext_OverrideWinsAndDefaultsPreserved(t *testing.T) {
	override := &corev1.PodSecurityContext{
		RunAsUser:    ptr.To(int64(1000)),
		RunAsNonRoot: ptr.To(false),
	}
	merged, err := MergePodSecurityContext(override)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Override wins
	if merged.RunAsUser == nil || *merged.RunAsUser != 1000 {
		t.Errorf("expected runAsUser=1000, got %v", merged.RunAsUser)
	}
	if merged.RunAsNonRoot == nil || *merged.RunAsNonRoot {
		t.Errorf("expected runAsNonRoot override=false, got %v", merged.RunAsNonRoot)
	}
	// Unset field falls back to default
	if merged.SeccompProfile == nil || merged.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Errorf("expected default seccompProfile preserved, got %v", merged.SeccompProfile)
	}
}

func TestMergeContainerSecurityContext_NilReturnsDefaults(t *testing.T) {
	merged, err := MergeContainerSecurityContext(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if merged.AllowPrivilegeEscalation == nil || *merged.AllowPrivilegeEscalation {
		t.Errorf("expected allowPrivilegeEscalation=false, got %v", merged.AllowPrivilegeEscalation)
	}
	if merged.Capabilities == nil || len(merged.Capabilities.Drop) != 1 || merged.Capabilities.Drop[0] != "ALL" {
		t.Errorf("expected capabilities drop [ALL], got %v", merged.Capabilities)
	}
}

func TestMergeContainerSecurityContext_OverrideWinsAndDefaultsPreserved(t *testing.T) {
	override := &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr.To(true),
	}
	merged, err := MergeContainerSecurityContext(override)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if merged.AllowPrivilegeEscalation == nil || !*merged.AllowPrivilegeEscalation {
		t.Errorf("expected allowPrivilegeEscalation override=true, got %v", merged.AllowPrivilegeEscalation)
	}
	// Other defaults preserved
	if merged.Capabilities == nil || len(merged.Capabilities.Drop) != 1 || merged.Capabilities.Drop[0] != "ALL" {
		t.Errorf("expected default capabilities drop [ALL] preserved, got %v", merged.Capabilities)
	}
}

func TestMergeContainerSecurityContext_CapabilitiesReplaced(t *testing.T) {
	override := &corev1.SecurityContext{
		Capabilities: &corev1.Capabilities{
			Add:  []corev1.Capability{"NET_BIND_SERVICE"},
			Drop: []corev1.Capability{"CHOWN"},
		},
	}
	merged, err := MergeContainerSecurityContext(override)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// User-supplied capabilities list replaces the default drop:[ALL]
	if len(merged.Capabilities.Drop) != 1 || merged.Capabilities.Drop[0] != "CHOWN" {
		t.Errorf("expected capabilities drop [CHOWN], got %v", merged.Capabilities.Drop)
	}
	if len(merged.Capabilities.Add) != 1 || merged.Capabilities.Add[0] != "NET_BIND_SERVICE" {
		t.Errorf("expected capabilities add [NET_BIND_SERVICE], got %v", merged.Capabilities.Add)
	}
}

func TestMergeDoesNotMutateDefaults(t *testing.T) {
	override := &corev1.SecurityContext{AllowPrivilegeEscalation: ptr.To(true)}
	if _, err := MergeContainerSecurityContext(override); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// A subsequent call with nil must still yield the pristine defaults.
	fresh, err := MergeContainerSecurityContext(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fresh.AllowPrivilegeEscalation == nil || *fresh.AllowPrivilegeEscalation {
		t.Errorf("defaults were mutated: expected allowPrivilegeEscalation=false, got %v", fresh.AllowPrivilegeEscalation)
	}
}
