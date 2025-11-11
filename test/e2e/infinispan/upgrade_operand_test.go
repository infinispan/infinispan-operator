package infinispan

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/blang/semver"
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

// TestOperandInPlaceRollingUpgrade tests InPlaceRolling upgrade type (Operator executes StatefulSet rolling upgrade with the same minor version).
func TestOperandInPlaceRollingUpgrade(t *testing.T) {
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)
	versionManager := tutils.VersionManager()
	replicas := 3

	// Retrieve the oldest release in the stream for the current minor.
	latestOperand := versionManager.Latest()
	minor := fmt.Sprintf("%d.%d.0", latestOperand.UpstreamVersion.Major, latestOperand.UpstreamVersion.Minor)
	operand, err := oldestUpstreamPatch(*versionManager, semver.MustParse(minor))
	tutils.ExpectNoError(err)

	if operand.Ref() == latestOperand.Ref() {
		t.Skip("Cannot execute test due to insufficient number of versions in the stream")
	}

	tutils.Log().Infoln("InPlace rolling upgrade will be performed from", operand.Ref(), "to", latestOperand.Ref())

	spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Replicas = int32(replicas)
		i.Spec.Version = operand.Ref()
		i.Spec.Upgrades = &ispnv1.InfinispanUpgradesSpec{
			Type: ispnv1.UpgradeTypeInPlaceRolling,
		}
	})

	modifier := func(ispn *ispnv1.Infinispan) {
		// Update the spec to install latest operand
		ispn.Spec.Version = latestOperand.Ref()
	}

	verifier := func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		// Ensure that the Operand Phase is eventually set to Running
		testKube.WaitForInfinispanState(ispn.Name, ispn.Namespace, func(i *ispnv1.Infinispan) bool {
			return i.IsConditionTrue(ispnv1.ConditionWellFormed) &&
				i.Status.Operand.Version == latestOperand.Ref() &&
				i.Status.Operand.Image == latestOperand.Image &&
				i.Status.Operand.Phase == ispnv1.OperandPhaseRunning
		})

		assert.Equal(t, ss.Spec.Template.Spec.Containers[0].Image, latestOperand.Image)
	}

	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(int(spec.Spec.Replicas), tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	client := tutils.HTTPClientForClusterWithVersionManager(spec, testKube, versionManager)
	inplaceCacheHelper := tutils.NewCacheHelper("inplacecache", client)
	indexedCacheHelper := tutils.NewCacheHelper("inplace-indexed", client)

	inplaceCacheHelper.CreateAndPopulateVolatileCache(1000)
	indexedCacheHelper.CreateAndPopulateIndexedCache(1000)

	tutils.Log().Infoln("Performing InPlace rolling upgrade from ", operand.Ref(), " to ", latestOperand.Ref())
	verifyStatefulSetUpdate(*spec, modifier, verifier)

	inplaceCacheHelper.AssertSize(1000)
	indexedCacheHelper.AssertSize(1000)
}

// OldestUpstreamPatch returns the oldest Operand patch release associated with the upstream stream, e.g. 15.0.x
func oldestUpstreamPatch(m version.Manager, stream semver.Version) (version.Operand, error) {
	for _, v := range m.Operands {
		if v.UpstreamVersion.Major == stream.Major && v.UpstreamVersion.Minor == stream.Minor {
			return *v, nil
		}
	}

	return version.Operand{}, fmt.Errorf("No Operands found in upstream stream '%d.%d'", stream.Major, stream.Minor)
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

// Tests that Operator is creating correct RemoteStore configurations according to a version in use
func TestOperandsHotRodRolling(t *testing.T) {
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)
	log := tutils.Log()
	versionManager := tutils.VersionManager()

	replicas := 2
	entriesPerCache := 100
	spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Replicas = int32(replicas)
		i.Spec.Service.Container.EphemeralStorage = false
		i.Spec.Upgrades = &ispnv1.InfinispanUpgradesSpec{
			Type: ispnv1.UpgradeTypeHotRodRolling,
		}
		i.Spec.Security.Authorization = &ispnv1.Authorization{
			Enabled: true,
		}
		i.Spec.Version = versionManager.Oldest().Ref()
		i.Spec.ConfigListener.Enabled = true
	})

	testKube.Create(spec)
	testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	spec = testKube.WaitForInfinispanCondition(spec.Name, tutils.Namespace, ispnv1.ConditionWellFormed)
	client := tutils.HTTPClientForClusterWithVersionManager(spec, testKube, versionManager)

	// Create caches
	textCacheHelper := tutils.NewCacheHelper("textCache", client)
	textCacheHelper.CreateDistributedCache(mime.TextPlain)
	textCacheHelper.PopulatePlainCache(entriesPerCache)

	jsonCacheHelper := tutils.NewCacheHelper("jsonCache", client)
	jsonCacheHelper.CreateDistributedCache(mime.ApplicationJson)

	javaCacheHelper := tutils.NewCacheHelper("javaCache", client)
	javaCacheHelper.CreateDistributedCache(mime.ApplicationJavaObject)

	clusterCounter := 0
	newStatefulSetName := spec.Name
	indexedCacheName := "IndexedCache"

	for i := 1; i < len(versionManager.Operands); i++ {
		operand := versionManager.Operands[i]

		if !tutils.IsTestOperand(*operand) {
			continue
		}

		assertMigration := func(expectedImage string, isRollingUpgrade, indexedSupported bool) {
			if !isRollingUpgrade {
				clusterCounter++
				currentStatefulSetName := newStatefulSetName
				newStatefulSetName = fmt.Sprintf("%s-%d", spec.Name, clusterCounter)

				testKube.WaitForStateFulSet(newStatefulSetName, tutils.Namespace)
				testKube.WaitForStateFulSetRemoval(currentStatefulSetName, tutils.Namespace)
				testKube.WaitForInfinispanPodsCreatedBy(0, tutils.SinglePodTimeout, currentStatefulSetName, tutils.Namespace)
				testKube.WaitForInfinispanState(spec.Name, spec.Namespace, func(i *ispnv1.Infinispan) bool {
					return i.Status.StatefulSetName == newStatefulSetName
				})
			}
			// Assert that the pods in the target StatefulSet are using the expected image
			targetPods := testKube.WaitForInfinispanPodsCreatedBy(replicas, tutils.SinglePodTimeout, newStatefulSetName, tutils.Namespace)
			for _, pod := range targetPods.Items {
				if pod.Spec.Containers[0].Image != expectedImage {
					panic(fmt.Errorf("upgraded image [%v] in Target StatefulSet Pod not equal desired cluster image [%v]", pod.Spec.Containers[0].Image, expectedImage))
				}
			}

			operand := tutils.Operand(spec.Spec.Version, versionManager)
			if !tutils.CheckExternalAddress(client, operand) {
				panic("Error contacting server")
			}

			// Check data
			assert.Equal(t, entriesPerCache, textCacheHelper.Size())
			assert.Equal(t, 0, jsonCacheHelper.Size())
			assert.Equal(t, 0, javaCacheHelper.Size())

			if indexedSupported {
				assert.Equal(t, entriesPerCache, tutils.NewCacheHelper(indexedCacheName, client).Size())
			}
		}

		conditionTimeout := 2 * tutils.ConditionWaitTimeout
		ispnPreUpgrade := testKube.WaitForInfinispanConditionWithTimeout(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed, conditionTimeout)
		currentOperand, err := versionManager.WithRef(ispnPreUpgrade.Spec.Version)
		client = tutils.HTTPClientForClusterWithVersionManager(spec, testKube, versionManager)
		tutils.ExpectNoError(err)

		if !currentOperand.EQ(*operand) {
			ispn := testKube.WaitForInfinispanConditionWithTimeout(spec.Name, tutils.Namespace, ispnv1.ConditionWellFormed, conditionTimeout)

			// ISPN-15651 Test migrating Indexed caches from 14.0.25.Final onwards
			indexSupported := operand.UpstreamVersion.GTE(*version.Operand{UpstreamVersion: &semver.Version{Major: 14, Minor: 0, Patch: 25}}.UpstreamVersion)
			if indexSupported {
				tutils.NewCacheHelper(indexedCacheName, client).CreateAndPopulateIndexedCache(entriesPerCache)
			}

			tutils.ExpectNoError(
				testKube.UpdateInfinispan(ispn, func() {
					ispn.Spec.Version = operand.Ref()
					log.Infof("Upgrading Operand to %s", ispn.Spec.Version)
				}),
			)
			testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
			testKube.WaitForInfinispanState(spec.Name, spec.Namespace, func(i *ispnv1.Infinispan) bool {
				return i.IsConditionTrue(ispnv1.ConditionWellFormed) &&
					i.Status.Operand.Version == operand.Ref() &&
					i.Status.Operand.Image == operand.Image &&
					i.Status.Operand.Phase == ispnv1.OperandPhaseRunning
			})

			lastOperand, err := versionManager.WithRef(ispnPreUpgrade.Status.Operand.Version)
			tutils.ExpectNoError(err)
			isRolling := operand.CVE && lastOperand.UpstreamVersion.EQ(*operand.UpstreamVersion)
			assertMigration(operand.Image, isRolling, indexSupported)
		}
	}

	// Ensure that the Cache ConfigListener is still able to receive updates after the upgrades has completed by creating
	// a cache on the server and waiting for it's corresponding Cache CR to be created.
	client = tutils.HTTPClientForClusterWithVersionManager(spec, testKube, versionManager)
	cacheName := "config-listener"
	cacheConfig := "localCache: \n  memory: \n    maxCount: \"100\"\n"
	cacheHelper := tutils.NewCacheHelper(cacheName, client)
	cacheHelper.Create(cacheConfig, mime.ApplicationYaml)
	testKube.WaitForCacheConditionReady(cacheName, spec.Name, tutils.Namespace)
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
// If two Operands are not available that support the /health/* endpoints, older Operands are provided
func specImageOperands() (*version.Operand, *version.Operand) {
	operands := tutils.VersionManager().Operands
	length := len(operands)

	var latest, latestMinus1 *version.Operand
	for i := 0; i < length-1; i++ {
		latest = operands[length-1-i]
		latestMinus1 = operands[length-2-i]
		if latest.UpstreamVersion.Major == latestMinus1.UpstreamVersion.Major &&
			latest.UpstreamVersion.Minor == latestMinus1.UpstreamVersion.Minor &&
			provision.DecoupledProbesSupported(*latest) == provision.DecoupledProbesSupported(*latestMinus1) {
			return latestMinus1, latest
		}
	}
	panic("We expect to have at least one operand that has the same major and minor version")
}
