package hotrod_rolling_upgrade

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	coreos "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	ctx              = context.TODO()
	conditionTimeout = 2 * tutils.ConditionWaitTimeout
	testKube         = tutils.NewTestKubernetes(os.Getenv("TESTING_CONTEXT"))
)

func TestMain(t *testing.M) {
	code := t.Run()
	os.Exit(code)
}

func TestRollingUpgrade(t *testing.T) {
	olm := testKube.OLMTestEnv()
	olm.PrintManifest()
	sourceChannel := olm.SourceChannel
	targetChannel := olm.TargetChannel

	testKube.NewNamespace(tutils.Namespace)
	sub := &coreos.Subscription{
		TypeMeta: metav1.TypeMeta{
			APIVersion: coreos.SubscriptionCRDAPIVersion,
			Kind:       coreos.SubscriptionKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      olm.SubName,
			Namespace: olm.SubNamespace,
		},
		Spec: &coreos.SubscriptionSpec{
			Channel:                olm.SourceChannel.Name,
			CatalogSource:          olm.CatalogSource,
			CatalogSourceNamespace: olm.CatalogSourceNamespace,
			InstallPlanApproval:    coreos.ApprovalManual,
			Package:                olm.SubPackage,
			StartingCSV:            olm.SubStartingCSV,
		},
	}

	defer testKube.CleanupOLMTest(t, tutils.TestName(t), olm.SubName, olm.SubNamespace, olm.SubPackage)
	testKube.CreateSubscriptionAndApproveInitialVersion(olm, sub)

	replicas := 2
	entriesPerCache := 100
	spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Replicas = int32(replicas)
		i.Spec.Container.CPU = "1000m"
		i.Spec.Service.Container.EphemeralStorage = false
		i.Spec.Upgrades = &ispnv1.InfinispanUpgradesSpec{
			Type: ispnv1.UpgradeTypeHotRodRolling,
		}
	})
	// Explicitly reset the Version so that it will be set by the Operator webhook
	spec.Spec.Version = ""

	testKube.Create(spec)
	testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	spec = testKube.WaitForInfinispanCondition(spec.Name, tutils.Namespace, ispnv1.ConditionWellFormed)
	client := tutils.HTTPClientForCluster(spec, testKube)

	// Create caches
	createCache("textCache", mime.TextPlain, client)
	createCache("jsonCache", mime.ApplicationJson, client)
	createCache("javaCache", mime.ApplicationJavaObject, client)
	createCache("indexedCache", mime.ApplicationProtostream, client)

	// Add data to some caches
	addData("textCache", entriesPerCache, client)
	addData("indexedCache", entriesPerCache, client)

	// Upgrade the Subscription channel if required
	if sourceChannel != targetChannel {
		testKube.UpdateSubscriptionChannel(targetChannel.Name, sub)
	}

	clusterCounter := 0
	newStatefulSetName := spec.Name

	// Approve InstallPlans and verify cluster state on each upgrade until the most recent CSV has been reached
	for testKube.Subscription(sub); sub.Status.InstalledCSV != targetChannel.CurrentCSVName; {
		fmt.Printf("Installed csv: %s, Current CSV: %s\n", sub.Status.InstalledCSV, targetChannel.CurrentCSVName)
		ispnPreUpgrade := testKube.WaitForInfinispanConditionWithTimeout(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed, conditionTimeout)
		testKube.WaitForSubscriptionState(coreos.SubscriptionStateUpgradePending, sub)
		testKube.ApproveInstallPlan(sub)

		testKube.WaitForSubscription(sub, func() bool {
			return sub.Status.InstalledCSV == sub.Status.CurrentCSV
		})
		testKube.WaitForCSVSucceeded(sub)
		// Operator does not start properly on the first attempt after the upgrade and is restarted
		// https://github.com/infinispan/infinispan-operator/issues/1719
		time.Sleep(time.Minute)

		operands := func() *version.Manager {
			testKube.SetRelatedImagesEnvs(sub)
			operandVersions := testKube.InstalledCSVEnv(ispnv1.OperatorOperandVersionEnvVarName, sub)
			if operandVersions == "" {
				panic(fmt.Sprintf("%s env empty, cannot continue", ispnv1.OperatorOperandVersionEnvVarName))
			}
			versionManager, err := version.ManagerFromJson(operandVersions)
			tutils.ExpectNoError(err)
			return versionManager
		}

		assertMigration := func(expectedImage string, isRollingUpgrade bool) {
			if !isRollingUpgrade {
				clusterCounter++
				currentStatefulSetName := newStatefulSetName
				newStatefulSetName := fmt.Sprintf("%s-%d", spec.Name, clusterCounter)

				testKube.WaitForStateFulSet(newStatefulSetName, tutils.Namespace)
				testKube.WaitForStateFulSetRemoval(currentStatefulSetName, tutils.Namespace)
				testKube.WaitForInfinispanPodsCreatedBy(0, tutils.SinglePodTimeout, currentStatefulSetName, tutils.Namespace)
			}
			// Assert that the pods in the target StatefulSet are using the expected image
			targetPods := testKube.WaitForInfinispanPodsCreatedBy(replicas, tutils.SinglePodTimeout, newStatefulSetName, tutils.Namespace)
			for _, pod := range targetPods.Items {
				if pod.Spec.Containers[0].Image != expectedImage {
					panic(fmt.Errorf("upgraded image [%v] in Target StatefulSet Pod not equal desired cluster image [%v]", pod.Spec.Containers[0].Image, expectedImage))
				}
			}

			if !tutils.CheckExternalAddress(client) {
				panic("Error contacting server")
			}

			// Check data
			assert.Equal(t, entriesPerCache, cacheSize("textCache", client))
			assert.Equal(t, entriesPerCache, cacheSize("indexedCache", client))
			assert.Equal(t, 0, cacheSize("jsonCache", client))
			assert.Equal(t, 0, cacheSize("javaCache", client))
		}

		if ispnPreUpgrade.Spec.Version == "" {
			relatedImageJdk := testKube.InstalledCSVEnv("RELATED_IMAGE_OPENJDK", sub)
			if relatedImageJdk != "" {
				// The latest Operator version still doesn't support multi-operand so check that the RELATED_IMAGE_OPENJDK
				// image has been installed on all pods
				assertMigration(relatedImageJdk, false)
				continue
			}

			// This is the first upgrade to an Operator with multi-operand support, so wait for the oldest Operand
			oldestOperand := operands().Oldest()
			ispnPreUpgrade = testKube.WaitForInfinispanState(spec.Name, spec.Namespace, func(i *ispnv1.Infinispan) bool {
				return i.IsConditionTrue(ispnv1.ConditionWellFormed) &&
					i.Status.Operand.Version == oldestOperand.Ref() &&
					i.Status.Operand.Image == oldestOperand.Image &&
					i.Status.Operand.Phase == ispnv1.OperandPhaseRunning
			})
			pods := &corev1.PodList{}
			err := testKube.Kubernetes.ResourcesList(tutils.Namespace, spec.PodSelectorLabels(), pods, ctx)
			tutils.ExpectNoError(err)
			for _, pod := range pods.Items {
				if pod.Spec.Containers[0].Image != oldestOperand.Image {
					panic(fmt.Errorf("upgraded image [%v] in Pod not equal desired cluster image [%v]", pod.Spec.Containers[0].Image, oldestOperand.Image))
				}
			}
		}

		latestOperand := operands().Latest()
		if ispnPreUpgrade.Spec.Version != latestOperand.Ref() {
			ispn := testKube.WaitForInfinispanConditionWithTimeout(spec.Name, tutils.Namespace, ispnv1.ConditionWellFormed, conditionTimeout)
			tutils.ExpectNoError(
				testKube.UpdateInfinispan(ispn, func() {
					ispn.Spec.Version = latestOperand.Ref()
				}),
			)
			testKube.WaitForInfinispanState(spec.Name, spec.Namespace, func(i *ispnv1.Infinispan) bool {
				return i.IsConditionTrue(ispnv1.ConditionWellFormed) &&
					i.Status.Operand.Version == latestOperand.Ref() &&
					i.Status.Operand.Image == latestOperand.Image &&
					i.Status.Operand.Phase == ispnv1.OperandPhasePending
			})
			testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
			testKube.WaitForInfinispanState(spec.Name, spec.Namespace, func(i *ispnv1.Infinispan) bool {
				return i.IsConditionTrue(ispnv1.ConditionWellFormed) &&
					i.Status.Operand.Version == latestOperand.Ref() &&
					i.Status.Operand.Image == latestOperand.Image
				// https://github.com/infinispan/infinispan-operator/issues/1720
				// && i.Status.Operand.Phase == ispnv1.OperandPhaseRunning
			})
			lastOperand, err := operands().WithRef(ispnPreUpgrade.Status.Operand.Version)
			tutils.ExpectNoError(err)
			assertMigration(latestOperand.Image, latestOperand.CVE && lastOperand.UpstreamVersion.EQ(*latestOperand.UpstreamVersion))
		}
	}
}

func cacheSize(cacheName string, client tutils.HTTPClient) int {
	return tutils.NewCacheHelper(cacheName, client).Size()
}

func createCache(cacheName string, encoding mime.MimeType, client tutils.HTTPClient) {
	config := fmt.Sprintf("{\"distributed-cache\":{\"mode\":\"SYNC\",\"remote-timeout\": 60000,\"encoding\":{\"media-type\":\"%s\"}}}", encoding)
	tutils.NewCacheHelper(cacheName, client).Create(config, mime.ApplicationJson)
}

// addData Populates a cache with bounded parallelism
func addData(cacheName string, entries int, client tutils.HTTPClient) {
	cache := tutils.NewCacheHelper(cacheName, client)
	for i := 0; i < entries; i++ {
		data := strconv.Itoa(i)
		cache.Put(data, data, mime.TextPlain)
	}
	fmt.Printf("Populated cache %s with %d entries\n", cacheName, entries)
}
