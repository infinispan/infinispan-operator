package infinispan

import (
	"context"
	"strings"
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
	"github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	"github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan/handler/provision"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

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
	oldest := versionManager.Oldest()
	tutils.Log().Info(t.Name() + " will be performed from " + oldest.Ref())

	// Create Infinispan Cluster using the oldest Operand release
	replicas := 1
	spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Replicas = int32(replicas)
		i.Spec.Version = oldest.Ref()
	})
	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	ispn := testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	if oldest.Deprecated {
		assert.True(t, ispn.Status.Operand.Deprecated)
	}

	// Follow the support Operand graph, updating the cluster to each non-cve release
	for i := 1; i < len(versionManager.Operands); i++ {
		// Update the Infinispan spec to use the next supported Operand
		operand := versionManager.Operands[i]
		if operand.CVE {
			continue
		}
		tutils.Log().Info("Updating version to " + operand.Ref())
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
	operand := versionManager.Operands[len(versionManager.Operands)-2]
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

func TestOperandHotRodRolling(t *testing.T) {
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)
	versionManager := tutils.VersionManager()

	// Create Infinispan Cluster using first Operand and then upgrade to the next Operand that is not marked as a CVE as
	// we want to ensure that a StatefulSet rolling upgrade does not occur.
	// The actual Operand versions deployed should not impact the test as we're only verifying the HR upgrade procedure
	// which is controlled by the Operator
	replicas := 1
	operands := versionManager.Operands
	startingOperand := operands[0]
	var upgradeOperand *version.Operand
	for i := 1; i < len(operands); i++ {
		op := operands[i]
		if !op.CVE {
			upgradeOperand = operands[i]
			break
		}
	}
	assert.NotNil(t, upgradeOperand)

	ispn := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.ConfigListener.Enabled = true
		i.Spec.Replicas = int32(replicas)
		i.Spec.Version = startingOperand.Ref()
		i.Spec.Upgrades = &ispnv1.InfinispanUpgradesSpec{
			Type: ispnv1.UpgradeTypeHotRodRolling,
		}
	})

	originalStatefulSetName := ispn.Name
	testKube.CreateInfinispan(ispn, tutils.Namespace)
	testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, ispn.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, ispnv1.ConditionWellFormed)
	testKube.WaitForInfinispanState(ispn.Name, ispn.Namespace, func(i *ispnv1.Infinispan) bool {
		return i.IsConditionTrue(ispnv1.ConditionWellFormed) &&
			i.Status.Operand.Version == startingOperand.Ref() &&
			i.Status.Operand.Image == startingOperand.Image &&
			i.Status.Operand.Phase == ispnv1.OperandPhaseRunning &&
			i.Status.StatefulSetName == originalStatefulSetName
	})

	tutils.ExpectNoError(
		testKube.UpdateInfinispan(ispn, func() {
			ispn.Spec.Version = upgradeOperand.Ref()
			ispn.Default()
		}),
	)

	newStatefulSetName := ispn.Name + "-1"
	testKube.WaitForStateFulSetRemoval(originalStatefulSetName, tutils.Namespace)
	testKube.WaitForInfinispanPodsCreatedBy(0, tutils.SinglePodTimeout, originalStatefulSetName, tutils.Namespace)
	testKube.WaitForStateFulSet(newStatefulSetName, tutils.Namespace)
	testKube.WaitForInfinispanPodsCreatedBy(replicas, tutils.SinglePodTimeout, newStatefulSetName, tutils.Namespace)
	testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, ispnv1.ConditionWellFormed)
	testKube.WaitForInfinispanState(ispn.Name, ispn.Namespace, func(i *ispnv1.Infinispan) bool {
		return i.IsConditionTrue(ispnv1.ConditionWellFormed) &&
			i.Status.Operand.Version == upgradeOperand.Ref() &&
			i.Status.Operand.Image == upgradeOperand.Image &&
			i.Status.Operand.Phase == ispnv1.OperandPhaseRunning &&
			i.Status.StatefulSetName == newStatefulSetName
	})

	// Ensure that the Cache ConfigListener is still able to receive updates after the upgrade has completed by creating
	// a cache on the server and waiting for it's corresponding Cache CR to be created.
	client := tutils.HTTPClientForClusterWithVersionManager(ispn, testKube, versionManager)
	cacheName := "config-listener"
	cacheConfig := "localCache: \n  memory: \n    maxCount: \"100\"\n"
	cacheHelper := tutils.NewCacheHelper(cacheName, client)
	cacheHelper.Create(cacheConfig, mime.ApplicationYaml)
	testKube.WaitForCacheConditionReady(cacheName, ispn.Name, tutils.Namespace)
}

// TestOperandCVEHotRodRolling tests that Operands marked as CVE releases, with the same upstream version as the currently
// installed operand, only result in a StatefulSet rolling upgrade
func TestOperandCVEHotRodRolling(t *testing.T) {
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)
	versionManager := tutils.VersionManager()

	if !versionManager.Latest().CVE {
		t.Skip("Latest release is non-cve, skipping test")
	}

	// Create Infinispan Cluster using the penultimate Operand release
	replicas := 1
	operand := versionManager.Operands[len(versionManager.Operands)-2]
	spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Replicas = int32(replicas)
		i.Spec.Version = operand.Ref()
		i.Spec.Upgrades = &ispnv1.InfinispanUpgradesSpec{
			Type: ispnv1.UpgradeTypeHotRodRolling,
		}
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

func TestSpecImageUpdate(t *testing.T) {
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	// Create Infinispan Cluster using an older Operand release (first operand) as this will have a different image name
	// to a newer release (second operand). We can then manually specify spec.image using the FQN of the latest image to
	// simulate a user specifying custom images.
	// The two version are chosen to be the last possible versions having the same major and minor.
	replicas := 1
	firstOperand, secondOperand := specImageOperands()
	spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Replicas = int32(replicas)
		i.Spec.Version = firstOperand.Ref()
	})

	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	ispn := testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	customImage := secondOperand.Image
	tutils.ExpectNoError(
		testKube.UpdateInfinispan(ispn, func() {
			// Update the spec to install the custom image
			ispn.Spec.Image = pointer.String(customImage)
		}),
	)

	testKube.WaitForInfinispanState(spec.Name, spec.Namespace, func(i *ispnv1.Infinispan) bool {
		return i.IsConditionTrue(ispnv1.ConditionWellFormed) &&
			i.Status.Operand.CustomImage &&
			i.Status.Operand.Version == firstOperand.Ref() &&
			i.Status.Operand.Image == customImage &&
			i.Status.Operand.Phase == ispnv1.OperandPhaseRunning
	})

	// Ensure that the newly created cluster pods have the correct Operand image
	podList := &corev1.PodList{}
	tutils.ExpectNoError(testKube.Kubernetes.ResourcesList(tutils.Namespace, ispn.PodSelectorLabels(), podList, context.TODO()))
	for _, pod := range podList.Items {
		container := kubernetes.GetContainer(provision.InfinispanContainer, &pod.Spec)
		assert.Equal(t, customImage, container.Image)
	}

	// Ensure that the StatefulSet is on its first generation, i.e. a RollingUpgrade has not been performed
	ss := appsv1.StatefulSet{}
	tutils.ExpectNoError(testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: ispn.Namespace, Name: ispn.GetStatefulSetName()}, &ss))
	assert.EqualValues(t, 1, ss.Status.ObservedGeneration)

	latestOperand := secondOperand
	tutils.ExpectNoError(
		testKube.UpdateInfinispan(ispn, func() {
			// Update the spec to move to the latest Operand version to ensure that a new GracefulShutdown is triggered
			ispn.Spec.Image = nil
			ispn.Spec.Version = latestOperand.Ref()
		}),
	)

	testKube.WaitForInfinispanState(spec.Name, spec.Namespace, func(i *ispnv1.Infinispan) bool {
		return i.IsConditionTrue(ispnv1.ConditionWellFormed) &&
			!i.Status.Operand.CustomImage &&
			i.Status.Operand.Version == latestOperand.Ref() &&
			i.Status.Operand.Image == latestOperand.Image &&
			i.Status.Operand.Phase == ispnv1.OperandPhaseRunning
	})

	// Ensure that the StatefulSet is on its first generation, i.e. a RollingUpgrade has not been performed
	ss = appsv1.StatefulSet{}
	tutils.ExpectNoError(testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: ispn.Namespace, Name: ispn.GetStatefulSetName()}, &ss))
	assert.EqualValues(t, 1, ss.Status.ObservedGeneration)

	tutils.ExpectNoError(
		testKube.UpdateInfinispan(ispn, func() {
			// Update the spec to move to back to the penultimate Operand version to ensure that an upgrade is still
			// triggered when the Operand is marked as CVE=true
			ispn.Spec.Image = pointer.String(firstOperand.Image)
		}),
	)

	testKube.WaitForInfinispanState(spec.Name, spec.Namespace, func(i *ispnv1.Infinispan) bool {
		return i.IsConditionTrue(ispnv1.ConditionWellFormed) &&
			i.Status.Operand.CustomImage &&
			i.Status.Operand.Version == latestOperand.Ref() &&
			i.Status.Operand.Image == firstOperand.Image &&
			i.Status.Operand.Phase == ispnv1.OperandPhaseRunning
	})

	// Ensure that the StatefulSet is on its first generation, i.e. a RollingUpgrade has not been performed
	ss = appsv1.StatefulSet{}
	tutils.ExpectNoError(testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: ispn.Namespace, Name: ispn.GetStatefulSetName()}, &ss))
	assert.EqualValues(t, 1, ss.Status.ObservedGeneration)
}

// TestPodAlreadyShutdownOnUpgrade simulates a scenario where a GracefulShutdown fails when only a subset of pods have had
// their container shutdown
func TestPodAlreadyShutdownOnUpgrade(t *testing.T) {
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	i := tutils.DefaultSpec(t, testKube, func(infinispan *ispnv1.Infinispan) {
		infinispan.Spec.Replicas = 1
		infinispan.Spec.Security.EndpointAuthentication = pointer.Bool(false)
	})
	testKube.CreateInfinispan(i, tutils.Namespace)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, i.Name, tutils.Namespace)
	i = testKube.WaitForInfinispanCondition(i.Name, i.Namespace, ispnv1.ConditionWellFormed)

	schema := i.GetEndpointScheme()
	client_ := testKube.WaitForExternalService(i, tutils.RouteTimeout, tutils.NewHTTPClientNoAuth(schema), nil)
	ispnClient := client.New(tutils.CurrentOperand, client_)
	tutils.ExpectNoError(
		ispnClient.Container().Shutdown(),
	)

	tutils.ExpectNoError(
		testKube.UpdateInfinispan(i, func() {
			i.Spec.Replicas = 0
		}),
	)
	testKube.WaitForInfinispanPods(0, tutils.SinglePodTimeout, i.Name, tutils.Namespace)

	ss := appsv1.StatefulSet{}
	tutils.ExpectNoError(
		testKube.Kubernetes.Client.Get(
			context.TODO(),
			types.NamespacedName{
				Namespace: i.Namespace,
				Name:      i.GetStatefulSetName(),
			},
			&ss,
		),
	)
	assert.EqualValues(t, int64(2), ss.Status.ObservedGeneration)
	assert.EqualValues(t, int32(0), ss.Status.ReadyReplicas)
	assert.EqualValues(t, int32(0), ss.Status.CurrentReplicas)
	assert.EqualValues(t, int32(0), ss.Status.UpdatedReplicas)
}

func TestScaleDownBlockedWithDegradedCache(t *testing.T) {
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	replicas := 1
	ispn := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Replicas = int32(replicas)
	})
	testKube.CreateInfinispan(ispn, tutils.Namespace)
	testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, ispn.Name, tutils.Namespace)
	ispn = testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, ispnv1.ConditionWellFormed)

	_client := tutils.HTTPClientForClusterWithVersionManager(ispn, testKube, tutils.VersionManager())
	cacheName := "cache"
	cacheConfig := "<distributed-cache><partition-handling when-split=\"DENY_READ_WRITES\" merge-policy=\"PREFERRED_ALWAYS\"/></distributed-cache>"
	cacheHelper := tutils.NewCacheHelper(cacheName, _client)
	cacheHelper.Create(cacheConfig, mime.ApplicationXml)
	cacheHelper.Available(false)

	tutils.ExpectNoError(
		testKube.UpdateInfinispan(ispn, func() {
			ispn.Spec.Replicas = 0
		}),
	)
	testKube.WaitForInfinispanState(ispn.Name, ispn.Namespace, func(i *ispnv1.Infinispan) bool {
		c := i.GetCondition(ispnv1.ConditionStopping)
		return c.Status == metav1.ConditionFalse && strings.Contains(c.Message, "unable to proceed with GracefulShutdown as the cluster health is 'DEGRADED'")
	})
	cacheHelper.Available(true)
	testKube.WaitForInfinispanPods(0, tutils.SinglePodTimeout, ispn.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, ispnv1.ConditionGracefulShutdown)
}

// specImageOperands() returns two latest Operands with the matching major/minor version
func specImageOperands() (*version.Operand, *version.Operand) {
	operands := tutils.VersionManager().Operands
	length := len(operands)

	var latest, latestMinus1 *version.Operand
	for i := 0; i < length-1; i++ {
		latest = operands[length-1-i]
		latestMinus1 = operands[length-2-i]
		if latest.UpstreamVersion.Major == latestMinus1.UpstreamVersion.Major &&
			latest.UpstreamVersion.Minor == latestMinus1.UpstreamVersion.Minor {
			return latestMinus1, latest
		}
	}
	panic("We expect to have at least one operand that has the same major and minor version")
}
