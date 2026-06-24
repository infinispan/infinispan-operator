package upgrade

import (
	"strings"
	"testing"
	"time"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	coreos "github.com/operator-framework/api/pkg/operators/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestUpgradeFromLatestReleased(t *testing.T) {
	olm := testKube.OLMTestEnv()
	olm.SubStartingCSV = testKube.GetLatestReleasedCSV()

	testUpgrade(t, olm)
}

// TestUpgrade ensures that the OLM upgrade graph can be traversed and Operands installed as expected
func TestUpgrade(t *testing.T) {
	olm := testKube.OLMTestEnv()

	testUpgrade(t, olm)
}

func testUpgrade(t *testing.T, olm tutils.OLMEnv) {
	log := tutils.Log()
	olm.PrintManifest()
	sourceChannel := olm.SourceChannel
	targetChannel := olm.TargetChannel

	testKube.NewNamespace(tutils.Namespace)
	sub := subscription(olm)

	defer testKube.CleanupOLMTest(t, tutils.TestName(t), olm.SubName, olm.SubNamespace, olm.SubPackage)
	testKube.CreateOperatorGroup(olm)
	testKube.CreateSubscriptionAndApproveInitialVersion(sub)

	// Create the Infinispan CR
	replicas := 2
	spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Replicas = int32(replicas)
		i.Spec.Service.Container.EphemeralStorage = false
		i.Spec.Logging.Categories["org.infinispan.topology"] = ispnv1.LoggingLevelTrace
	})
	// Explicitly reset the Version so that it will be set by the Operator webhook
	spec.Spec.Version = ""

	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	spec = testKube.WaitForInfinispanConditionWithTimeout(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed, conditionTimeout)

	log.Infof("Initial Operand version: %s", spec.Spec.Version)
	versionManager := testKube.VersionManagerFromCSV(sub)

	numEntries := 100
	client := tutils.HTTPClientForClusterWithVersionManager(spec, testKube, versionManager)

	// Add a persistent cache with data to ensure contents can be read after upgrade(s)
	tutils.NewCacheHelper(persistentCacheName, client).CreateAndPopulatePersistentCache(numEntries)

	// Add a volatile cache with data to ensure contents can be backed up and then restored after upgrade(s)
	tutils.NewCacheHelper(volatileCacheName, client).CreateAndPopulateVolatileCache(numEntries)

	// Create Backup
	backup := createBackupAndWaitToSucceed(spec.Name, t)

	// Upgrade the Subscription channel if required
	if sourceChannel != targetChannel {
		testKube.UpdateSubscriptionChannel(targetChannel.Name, sub)
	}

	// Approve InstallPlans and verify cluster state on each upgrade until the most recent CSV has been reached
	for testKube.Subscription(sub); sub.Status.InstalledCSV != targetChannel.CurrentCSVName; {
		log.Infof("Installed csv: %s", sub.Status.InstalledCSV)
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

		versionManager = testKube.VersionManagerFromCSV(sub)

		// Upgrade to the latest available Operand
		latestOperand := versionManager.Latest()
		if ispnPreUpgrade.Spec.Version != latestOperand.Ref() {
			ispn := testKube.WaitForInfinispanConditionWithTimeout(spec.Name, tutils.Namespace, ispnv1.ConditionWellFormed, conditionTimeout)

			if !tutils.IsTestOperand(latestOperand) {
				continue
			}

			tutils.ExpectNoError(
				testKube.UpdateInfinispan(ispn, func() {
					ispn.Spec.Version = latestOperand.Ref()
					log.Infof("Upgrading Operand to %s", ispn.Spec.Version)
				}),
			)
			testKube.WaitForInfinispanState(spec.Name, spec.Namespace, func(i *ispnv1.Infinispan) bool {
				return !i.IsConditionTrue(ispnv1.ConditionWellFormed) &&
					i.Status.Operand.Version == latestOperand.Ref() &&
					i.Status.Operand.Image == latestOperand.Image &&
					i.Status.Operand.Phase == ispnv1.OperandPhasePending
			})
			testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
			testKube.WaitForInfinispanState(spec.Name, spec.Namespace, func(i *ispnv1.Infinispan) bool {
				return i.IsConditionTrue(ispnv1.ConditionWellFormed) &&
					i.Status.Operand.Version == latestOperand.Ref() &&
					i.Status.Operand.Image == latestOperand.Image &&
					i.Status.Operand.Phase == ispnv1.OperandPhaseRunning
			})
			assertOperandImage(latestOperand.Image, spec)

			// Ensure that persistent cache entries have survived the upgrade(s)
			// Refresh the hostAddr and client as the url will change if NodePort is used.
			client = tutils.HTTPClientForClusterWithVersionManager(spec, testKube, versionManager)
			tutils.NewCacheHelper(persistentCacheName, client).AssertSize(numEntries)

			// Restore the backup and ensure that the cache exists with the expected number of entries
			restoreName := "upgrade-restore-" + strings.ReplaceAll(strings.TrimLeft(sub.Status.CurrentCSV, olm.SubName+".v"), ".", "-")
			if restore, err := createRestoreAndWaitToSucceed(restoreName, backup, t); err != nil {
				tutils.ExpectNoError(
					ignoreRestoreError(sub.Status.InstalledCSV, &latestOperand, spec, restore, client, err),
				)
				// We must recreate the caches that should have been restored if the Restore CR had succeeded
				// so that the Backup CR executed in the next loop has the expected content
				tutils.NewCacheHelper(volatileCacheName, client).CreateAndPopulateVolatileCache(numEntries)
			}
		}
		tutils.NewCacheHelper(volatileCacheName, client).AssertSize(numEntries)
	}

	checkServicePorts(t, spec.Name)
	checkBatch(t, spec)

	// Check for any corruption that could be caused by upgrade and improper data migration
	// 1. Kill the first pod to ensure that the cluster can recover from failover after upgrade
	err := testKube.Kubernetes.Client.Delete(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.Name + "-0",
			Namespace: tutils.Namespace,
		},
	})
	tutils.ExpectNoError(err)
	testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	testKube.WaitForInfinispanConditionWithTimeout(spec.Name, tutils.Namespace, ispnv1.ConditionWellFormed, conditionTimeout)

	// Ensure that persistent cache entries still contain the expected numEntries
	versionManager = testKube.VersionManagerFromCSV(sub)
	client = tutils.HTTPClientForClusterWithVersionManager(spec, testKube, versionManager)
	tutils.NewCacheHelper(persistentCacheName, client).AssertSize(numEntries)

	// 2. Verify all the cluster can be shutdown and brough back up without any issues
	testKube.GracefulShutdownInfinispan(spec)
	testKube.WaitForInfinispanPods(0, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	testKube.GracefulRestartInfinispan(spec, int32(replicas), tutils.SinglePodTimeout)

	// Ensure that persistent cache entries still contain the expected numEntries
	versionManager = testKube.VersionManagerFromCSV(sub)
	client = tutils.HTTPClientForClusterWithVersionManager(spec, testKube, versionManager)
	tutils.NewCacheHelper(persistentCacheName, client).AssertSize(numEntries)
}
