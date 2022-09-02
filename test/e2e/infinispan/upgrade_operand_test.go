package infinispan

import (
	"context"
	"fmt"
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan/handler/provision"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// TestOperandUpgrade tests that changes to spec.version results in the existing cluster being GracefulShutdown and
// a new cluster created with the updated Operand version.
func TestOperandUpgrade(t *testing.T) {
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	versionManager := tutils.VersionManager()
	oldest := versionManager.Oldest().Ref()
	fmt.Println(t.Name() + " will be performed from " + oldest)

	// Create Infinispan Cluster using the oldest Operand release
	replicas := 1
	spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Replicas = int32(replicas)
		i.Spec.Version = oldest
		// Ensure that FIPS is disabled when testing 13.0.x Operand
		i.Spec.Container.CliExtraJvmOpts = "-Dcom.redhat.fips=false"
		i.Spec.Container.ExtraJvmOpts = "-Dcom.redhat.fips=false"
	})
	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	ispn := testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	// Follow the support Operand graph, updating the cluster to each non-cve release
	for i := 1; i < len(versionManager.Operands); i++ {
		// Update the Infinispan spec to use the next supported Operand
		operand := versionManager.Operands[i]
		if operand.CVE {
			continue
		}
		fmt.Println("Updating version to " + operand.Ref())
		tutils.ExpectNoError(
			testKube.UpdateInfinispan(ispn, func() {
				ispn.Spec.Version = operand.Ref()
			}),
		)
		testKube.WaitForInfinispanState(spec.Name, spec.Namespace, func(i *ispnv1.Infinispan) bool {
			return !i.IsConditionTrue(ispnv1.ConditionWellFormed) &&
				i.Status.Operand.Version == operand.Ref() &&
				i.Status.Operand.Image == operand.Image &&
				i.Status.Operand.Phase == ispnv1.OperandPhasePending
		})

		testKube.WaitForInfinispanState(spec.Name, spec.Namespace, func(i *ispnv1.Infinispan) bool {
			return i.IsConditionTrue(ispnv1.ConditionWellFormed) &&
				i.Status.Operand.Version == operand.Ref() &&
				i.Status.Operand.Image == operand.Image &&
				i.Status.Operand.Phase == ispnv1.OperandPhaseRunning
		})

		// Ensure that the newly created cluster pods have the correct Operand image
		podList := &corev1.PodList{}
		tutils.ExpectNoError(testKube.Kubernetes.ResourcesList(tutils.Namespace, ispn.PodSelectorLabels(), podList, context.TODO()))
		for _, pod := range podList.Items {
			container := kubernetes.GetContainer(provision.InfinispanContainer, &pod.Spec)
			assert.Equal(t, operand.Image, container.Image)
		}

		// Ensure that the StatefulSet is on its first generation, i.e. a RollingUpgrade has not been performed
		ss := appsv1.StatefulSet{}
		tutils.ExpectNoError(testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: ispn.Namespace, Name: ispn.GetStatefulSetName()}, &ss))
		assert.EqualValues(t, 1, ss.Status.ObservedGeneration)
	}
}

// TestOperandCVERollingUpgrade tests that Operands marked as CVE releases, with the same upstream version as the currently
// installed operand, only result in a StatefulSet rolling upgrade
func TestOperandCVERollingUpgrade(t *testing.T) {
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)
	versionManager := tutils.VersionManager()

	if !versionManager.Latest().CVE {
		t.Skip("Latest release is non-cve, skipping test")
	}

	// Create Infinispan Cluster using the penultimate Operand release
	replicas := 1
	operand := versionManager.Operands[1]
	spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Replicas = int32(replicas)
		i.Spec.Version = operand.Ref()
	})

	cveOperand := versionManager.Latest()
	modifier := func(ispn *ispnv1.Infinispan) {
		// Update the spec to install the CVE operand
		ispn.Spec.Version = cveOperand.Ref()
	}

	verifier := func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		// Ensure that the Operand Phase is eventually set to Running
		testKube.WaitForInfinispanState(ispn.Name, ispn.Namespace, func(i *ispnv1.Infinispan) bool {
			return i.IsConditionTrue(ispnv1.ConditionWellFormed) &&
				i.Status.Operand.Version == cveOperand.Ref() &&
				i.Status.Operand.Image == cveOperand.Image &&
				i.Status.Operand.Phase == ispnv1.OperandPhaseRunning
		})
	}
	genericTestForContainerUpdated(*spec, modifier, verifier)
}

// TestOperandCVEGracefulShutdown tests that Operands marked as a CVE release, but with a different upstream version to
// the currently installed operand, result in a GracefulShutdown upgrade
func TestOperandCVEGracefulShutdown(t *testing.T) {
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)
	versionManager := tutils.VersionManager()

	if !versionManager.Latest().CVE {
		t.Skip("Latest release is non-cve, skipping test")
	}

	// Create Infinispan Cluster using the oldest Operand release
	replicas := 1
	spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Replicas = int32(replicas)
		i.Spec.Version = versionManager.Operands[0].Ref()
		// Ensure that FIPS is disabled when testing 13.0.x Operand
		i.Spec.Container.CliExtraJvmOpts = "-Dcom.redhat.fips=false"
		i.Spec.Container.ExtraJvmOpts = "-Dcom.redhat.fips=false"
	})
	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	ispn := testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	cveOperand := versionManager.Latest()

	tutils.ExpectNoError(
		testKube.UpdateInfinispan(ispn, func() {
			ispn.Spec.Version = cveOperand.Ref()
		}),
	)
	testKube.WaitForInfinispanState(spec.Name, spec.Namespace, func(i *ispnv1.Infinispan) bool {
		return !i.IsConditionTrue(ispnv1.ConditionWellFormed) &&
			i.Status.Operand.Version == cveOperand.Ref() &&
			i.Status.Operand.Image == cveOperand.Image &&
			i.Status.Operand.Phase == ispnv1.OperandPhasePending
	})

	testKube.WaitForInfinispanState(spec.Name, spec.Namespace, func(i *ispnv1.Infinispan) bool {
		return i.IsConditionTrue(ispnv1.ConditionWellFormed) &&
			i.Status.Operand.Version == cveOperand.Ref() &&
			i.Status.Operand.Image == cveOperand.Image &&
			i.Status.Operand.Phase == ispnv1.OperandPhaseRunning
	})

	// Ensure that the newly created cluster pods have the correct Operand image
	podList := &corev1.PodList{}
	tutils.ExpectNoError(testKube.Kubernetes.ResourcesList(tutils.Namespace, ispn.PodSelectorLabels(), podList, context.TODO()))
	for _, pod := range podList.Items {
		container := kubernetes.GetContainer(provision.InfinispanContainer, &pod.Spec)
		assert.Equal(t, cveOperand.Image, container.Image)
	}

	// Ensure that the StatefulSet is on its first generation, i.e. a RollingUpgrade has not been performed
	ss := appsv1.StatefulSet{}
	tutils.ExpectNoError(testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: ispn.Namespace, Name: ispn.GetStatefulSetName()}, &ss))
	assert.EqualValues(t, 1, ss.Status.ObservedGeneration)
}
