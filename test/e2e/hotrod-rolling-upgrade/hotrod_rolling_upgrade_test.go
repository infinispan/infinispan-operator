package hotrod_rolling_upgrade

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/blang/semver"
	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	coreos "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const IndexedCacheName = "IndexedCache"

var (
	conditionTimeout = 2 * tutils.ConditionWaitTimeout
	testKube         = tutils.NewTestKubernetes(os.Getenv("TESTING_CONTEXT"))
)

func TestMain(t *testing.M) {
	code := t.Run()
	os.Exit(code)
}

func TestHotRodRollingUpgrade(t *testing.T) {
	log := tutils.Log()
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
			Config: coreos.SubscriptionConfig{
				Env: []corev1.EnvVar{
					{Name: "THREAD_DUMP_PRE_STOP", Value: "TRUE"},
				},
			},
		},
	}

	defer testKube.CleanupOLMTest(t, tutils.TestName(t), olm.SubName, olm.SubNamespace, olm.SubPackage)
	testKube.CreateOperatorGroup(olm)
	testKube.CreateSubscriptionAndApproveInitialVersion(sub)

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
		// TestHotRodRollingUpgrade tests are CPU limitted on the CI. Remove once
		// https://github.com/infinispan/infinispan/issues/15937 is fixed.
		i.Spec.Container.ExtraJvmOpts = "-Dorg.infinispan.threads.virtual=false"
	})
	// Explicitly reset the Version so that it will be set by the Operator webhook
	spec.Spec.Version = ""

	testKube.Create(spec)
	testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	spec = testKube.WaitForInfinispanCondition(spec.Name, tutils.Namespace, ispnv1.ConditionWellFormed)
	versionManager := testKube.VersionManagerFromCSV(sub)
	client := tutils.HTTPClientForClusterWithVersionManager(spec, testKube, versionManager)

	// Create caches
	textCacheHelper := tutils.NewCacheHelper("textCache", client)
	textCacheHelper.CreateDistributedCache(mime.TextPlain)
	textCacheHelper.PopulatePlainCache(entriesPerCache)

	jsonCacheHelper := tutils.NewCacheHelper("jsonCache", client)
	jsonCacheHelper.CreateDistributedCache(mime.ApplicationJson)

	javaCacheHelper := tutils.NewCacheHelper("javaCache", client)
	javaCacheHelper.CreateDistributedCache(mime.ApplicationJavaObject)

	// Upgrade the Subscription channel if required
	if sourceChannel != targetChannel {
		testKube.UpdateSubscriptionChannel(targetChannel.Name, sub)
	}

	clusterCounter := 0
	newStatefulSetName := spec.Name

	// Approve InstallPlans and verify cluster state on each upgrade until the most recent CSV has been reached
	for testKube.Subscription(sub); sub.Status.InstalledCSV != targetChannel.CurrentCSVName; {
		log.Infof("Installed csv: %s, Current CSV: %s", sub.Status.InstalledCSV, targetChannel.CurrentCSVName)
		ispnPreUpgrade := testKube.WaitForInfinispanConditionWithTimeout(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed, conditionTimeout)
		testKube.WaitForSubscriptionState(coreos.SubscriptionStateUpgradePending, sub)
		testKube.ApproveInstallPlan(sub)

		testKube.WaitForSubscription(sub, func() bool {
			return sub.Status.InstalledCSV == sub.Status.CurrentCSV
		})
		testKube.WaitForCSVSucceeded(sub)
		// Operator does not start properly on the first attempt after the upgrade and is restarted
		// https://github.com/infinispan/infinispan-operator/issues/1719
		time.Sleep(time.Second * 30)

		versionManager = testKube.VersionManagerFromCSV(sub)
		assertMigration := func(expectedImage string, isRollingUpgrade, indexedSupported bool) {
			if !isRollingUpgrade {
				clusterCounter++
				currentStatefulSetName := newStatefulSetName
				newStatefulSetName = fmt.Sprintf("%s-%d", spec.Name, clusterCounter)

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

			operand := tutils.Operand(spec.Spec.Version, versionManager)
			if !tutils.CheckExternalAddress(client, operand) {
				panic("Error contacting server")
			}

			// Check data
			assert.Equal(t, entriesPerCache, textCacheHelper.Size())
			assert.Equal(t, 0, jsonCacheHelper.Size())
			assert.Equal(t, 0, javaCacheHelper.Size())

			if indexedSupported {
				assert.Equal(t, entriesPerCache, tutils.NewCacheHelper(IndexedCacheName, client).Size())
			}
		}

		latestOperand := versionManager.Latest()
		currentOperand, err := versionManager.WithRef(ispnPreUpgrade.Spec.Version)
		client = tutils.HTTPClientForClusterWithVersionManager(spec, testKube, versionManager)
		tutils.ExpectNoError(err)
		if !currentOperand.EQ(latestOperand) {
			ispn := testKube.WaitForInfinispanConditionWithTimeout(spec.Name, tutils.Namespace, ispnv1.ConditionWellFormed, conditionTimeout)

			if !tutils.IsTestOperand(latestOperand) {
				continue
			}

			// ISPN-15651 Test migrating Indexed caches from 14.0.25.Final onwards
			indexSupported := latestOperand.UpstreamVersion.GTE(*version.Operand{UpstreamVersion: &semver.Version{Major: 14, Minor: 0, Patch: 25}}.UpstreamVersion)
			if indexSupported {
				tutils.NewCacheHelper(IndexedCacheName, client).CreateAndPopulateIndexedCache(entriesPerCache)
			}

			tutils.ExpectNoError(
				testKube.UpdateInfinispan(ispn, func() {
					ispn.Spec.Version = latestOperand.Ref()
					log.Infof("Upgrading Operand to %s", ispn.Spec.Version)
				}),
			)
			testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
			testKube.WaitForInfinispanState(spec.Name, spec.Namespace, func(i *ispnv1.Infinispan) bool {
				return i.IsConditionTrue(ispnv1.ConditionWellFormed) &&
					i.Status.Operand.Version == latestOperand.Ref() &&
					i.Status.Operand.Image == latestOperand.Image &&
					i.Status.Operand.Phase == ispnv1.OperandPhaseRunning
			})
			lastOperand, err := versionManager.WithRef(ispnPreUpgrade.Status.Operand.Version)
			tutils.ExpectNoError(err)
			isRolling := latestOperand.CVE && lastOperand.UpstreamVersion.EQ(*latestOperand.UpstreamVersion)
			assertMigration(latestOperand.Image, isRolling, indexSupported)
		}
	}
}
