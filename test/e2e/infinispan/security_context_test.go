package infinispan

import (
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

// TestSecurityContextConfig verifies defaults, that a user-supplied securityContext is deep-merged over the
// defaults (override wins, other defaults preserved), that the change triggers a rollout.
//
// NOTE: Reverting to defaults by removing the securityContext from the spec is currently unsupported as it
// conflicts with the requirement on no StatefulSet auto-rollouts on Operator upgrade.
func TestSecurityContextConfig(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	// Define and deploy cluster
	ispn := tutils.DefaultSpec(t, testKube, nil)

	testKube.CreateInfinispan(ispn, tutils.Namespace)
	testKube.WaitForInfinispanPods(int(ispn.Spec.Replicas), tutils.SinglePodTimeout, ispn.Name, tutils.Namespace)
	ispn = testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, ispnv1.ConditionWellFormed)

	// Assert defaults
	assert := assert.New(t)
	ss := testKube.GetStatefulSet(ispn.GetStatefulSetName(), ispn.Namespace)

	assertDefaultPodSecurityContext(assert, ss)
	assertDefaultContainerSecurityContext(assert, ss)

	assert.Nil(ispn.Spec.SecurityContext, "spec.securityContext must not be persisted when unset")
	assert.Nil(ispn.Spec.Container.SecurityContext, "spec.container.securityContext must not be persisted when unset")

	// Use overrides that OpenShift's restricted-v2 SCC admits: supplementalGroups is RunAsAny, and
	// readOnlyRootFilesystem is unconstrained by the SCC (setting it false matches current runtime
	// behaviour)
	var modifier = func(ispn *ispnv1.Infinispan) {
		ispn.Spec.SecurityContext = &corev1.PodSecurityContext{
			SupplementalGroups: []int64{1000},
		}
		ispn.Spec.Container.SecurityContext = &corev1.SecurityContext{
			ReadOnlyRootFilesystem: ptr.To(false),
		}
	}
	var verifier = func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		psc := ss.Spec.Template.Spec.SecurityContext
		if assert.NotNil(psc) {
			assert.Equal([]int64{1000}, psc.SupplementalGroups)
			assert.Equal(ptr.To(true), psc.RunAsNonRoot)
		}
		sc := ss.Spec.Template.Spec.Containers[0].SecurityContext
		if assert.NotNil(sc) {
			assert.Equal(ptr.To(false), sc.ReadOnlyRootFilesystem)
			assert.Equal(ptr.To(false), sc.AllowPrivilegeEscalation)
			if assert.NotNil(sc.Capabilities) {
				assert.Equal([]corev1.Capability{"ALL"}, sc.Capabilities.Drop)
			}
		}
	}
	verifyStatefulSetUpdate(*ispn, modifier, verifier)
}

// assertDefaultPodSecurityContext verifies the hardened pod-level defaults.
func assertDefaultPodSecurityContext(assert *assert.Assertions, ss *appsv1.StatefulSet) {
	psc := ss.Spec.Template.Spec.SecurityContext
	if assert.NotNil(psc, "pod securityContext should be set") {
		assert.Equal(ptr.To(true), psc.RunAsNonRoot)
		if assert.NotNil(psc.SeccompProfile) {
			assert.Equal(corev1.SeccompProfileTypeRuntimeDefault, psc.SeccompProfile.Type)
		}
	}
}

// assertDefaultContainerSecurityContext verifies the hardened container-level defaults.
func assertDefaultContainerSecurityContext(assert *assert.Assertions, ss *appsv1.StatefulSet) {
	sc := ss.Spec.Template.Spec.Containers[0].SecurityContext
	if assert.NotNil(sc, "container securityContext should be set") {
		assert.Equal(ptr.To(false), sc.AllowPrivilegeEscalation)
		assert.Equal(ptr.To(true), sc.RunAsNonRoot)
		if assert.NotNil(sc.Capabilities) {
			assert.Equal([]corev1.Capability{"ALL"}, sc.Capabilities.Drop)
		}
	}
}
