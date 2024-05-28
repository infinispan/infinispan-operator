package upgrade

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/blang/semver"
	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	v2 "github.com/infinispan/infinispan-operator/api/v2alpha1"
	"github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	"github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan/handler/provision"
	batchtest "github.com/infinispan/infinispan-operator/test/e2e/batch"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	coreos "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

var (
	ctx              = context.TODO()
	testKube         = tutils.NewTestKubernetes(os.Getenv("TESTING_CONTEXT"))
	conditionTimeout = 2 * tutils.ConditionWaitTimeout
)

func TestUpgrade(t *testing.T) {
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
	testKube.CreateSubscriptionAndApproveInitialVersion(olm, sub)

	// Create the Infinispan CR
	replicas := 2
	spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Replicas = int32(replicas)
		i.Spec.Service.Container.EphemeralStorage = false
		// Ensure that FIPS is disabled when testing 13.0.x Operand
		i.Spec.Container.CliExtraJvmOpts = "-Dcom.redhat.fips=false"
		i.Spec.Container.ExtraJvmOpts = "-Dcom.redhat.fips=false"
		i.Spec.Logging.Categories["org.infinispan.topology"] = ispnv1.LoggingLevelTrace
	})
	// Explicitly reset the Version so that it will be set by the Operator webhook
	spec.Spec.Version = ""

	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	testKube.WaitForInfinispanConditionWithTimeout(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed, conditionTimeout)

	// Add a persistent cache with data to ensure contents can be read after upgrade(s)
	numEntries := 100
	client := tutils.HTTPClientForCluster(spec, testKube)

	peristentCache := "persistentCache"
	cache := tutils.NewCacheHelper(peristentCache, client)
	config := `{"distributed-cache":{"mode":"SYNC", "persistence":{"file-store":{}}}}`
	cache.Create(config, mime.ApplicationJson)
	cache.Populate(numEntries)
	cache.AssertSize(numEntries)

	// Add a volatile cache with data to ensure contents can be backed up and then restored after upgrade(s)
	volatileCache := "volatileCache"
	createAndPopulateVolatileCache := func(client tutils.HTTPClient) {
		c := tutils.NewCacheHelper(volatileCache, client)
		if !c.Exists() {
			c.Create(`{"distributed-cache":{"mode":"SYNC"}}`, mime.ApplicationJson)
		}
		if c.Size() != numEntries {
			c.Populate(numEntries)
			c.AssertSize(numEntries)
		}
	}
	createAndPopulateVolatileCache(client)

	// Create Backup
	backup := &v2.Backup{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v2alpha1",
			Kind:       "Backup",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "upgrade-backup",
			Namespace: tutils.Namespace,
			Labels:    map[string]string{"test-name": t.Name()},
		},
		Spec: v2.BackupSpec{
			Cluster: spec.Name,
		},
	}
	testKube.Create(backup)
	testKube.WaitForValidBackupPhase(backup.Name, backup.Namespace, v2.BackupSucceeded)

	// Upgrade the Subscription channel if required
	if sourceChannel != targetChannel {
		testKube.UpdateSubscriptionChannel(targetChannel.Name, sub)
	}

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

		assertOperandImage := func(expectedImage string) {
			pods := &corev1.PodList{}
			err := testKube.Kubernetes.ResourcesList(tutils.Namespace, spec.PodSelectorLabels(), pods, ctx)
			tutils.ExpectNoError(err)
			for _, pod := range pods.Items {
				if pod.Spec.Containers[0].Image != expectedImage {
					panic(fmt.Errorf("upgraded image [%v] in Pod not equal desired cluster image [%v]", pod.Spec.Containers[0].Image, expectedImage))
				}
			}
		}

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

		if ispnPreUpgrade.Spec.Version == "" {
			relatedImageJdk := testKube.InstalledCSVEnv("RELATED_IMAGE_OPENJDK", sub)
			if relatedImageJdk != "" {
				// We're upgrading from an Infinispan version that does not have multi-operand support so expect cluster
				// GracefulShutdown upgrade to happen automatically
				testKube.WaitForInfinispanConditionWithTimeout(spec.Name, tutils.Namespace, ispnv1.ConditionStopping, conditionTimeout)
				testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
				testKube.WaitForInfinispanConditionWithTimeout(spec.Name, tutils.Namespace, ispnv1.ConditionWellFormed, conditionTimeout)

				// The latest Operator version still doesn't support multi-operand so check that the RELATED_IMAGE_OPENJDK
				// image has been installed on all pods
				assertOperandImage(relatedImageJdk)
				client = tutils.HTTPClientForCluster(spec, testKube)
				tutils.NewCacheHelper(peristentCache, client).AssertSize(numEntries)
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
			assertOperandImage(oldestOperand.Image)
		}

		// Upgrade to the latest available Operand
		latestOperand := operands().Latest()
		if ispnPreUpgrade.Spec.Version != latestOperand.Ref() {
			ispn := testKube.WaitForInfinispanConditionWithTimeout(spec.Name, tutils.Namespace, ispnv1.ConditionWellFormed, conditionTimeout)
			tutils.ExpectNoError(
				testKube.UpdateInfinispan(ispn, func() {
					ispn.Spec.Version = latestOperand.Ref()
					fmt.Printf("Upgrading Operand to %s\n", ispn.Spec.Version)
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
			assertOperandImage(latestOperand.Image)

			// Ensure that persistent cache entries have survived the upgrade(s)
			// Refresh the hostAddr and client as the url will change if NodePort is used.
			client = tutils.HTTPClientForCluster(spec, testKube)
			tutils.NewCacheHelper(peristentCache, client).AssertSize(numEntries)

			// Restore the backup and ensure that the cache exists with the expected number of entries
			restore := &v2.Restore{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "infinispan.org/v2alpha1",
					Kind:       "Restore",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "upgrade-restore-" + strings.ReplaceAll(strings.TrimLeft(sub.Status.CurrentCSV, olm.SubName+".v"), ".", "-"),
					Namespace: tutils.Namespace,
					Labels:    map[string]string{"test-name": t.Name()},
				},
				Spec: v2.RestoreSpec{
					Backup:  backup.Name,
					Cluster: spec.Name,
				},
			}
			testKube.Create(restore)

			if restore, err := waitForValidRestorePhase(restore.Name, restore.Namespace, v2.RestoreSucceeded); err != nil {
				tutils.ExpectNoError(
					ignoreRestoreError(sub.Status.InstalledCSV, &latestOperand, spec, restore, client, err),
				)
				// We must recreate the caches that should have been restored if the Restore CR had succeeded
				// so that the Backup CR executed in the next loop has the expected content
				createAndPopulateVolatileCache(client)
			}
		}
		tutils.NewCacheHelper(volatileCache, client).AssertSize(numEntries)
	}

	checkServicePorts(t, spec.Name)
	checkBatch(t, spec.Name)

	// Kill the first pod to ensure that the cluster can recover from failover after upgrade
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
	client = tutils.HTTPClientForCluster(spec, testKube)
	tutils.NewCacheHelper(peristentCache, client).AssertSize(numEntries)
}

func waitForValidRestorePhase(name, namespace string, phase v2.RestorePhase) (restore *v2.Restore, err error) {
	err = wait.Poll(10*time.Millisecond, tutils.TestTimeout, func() (bool, error) {
		restore = testKube.GetRestore(name, namespace)
		if restore.Status.Phase == v2.RestoreFailed {
			return true, fmt.Errorf("restore failed. Reason: %s", restore.Status.Reason)
		}
		return phase == restore.Status.Phase, nil
	})
	if err != nil {
		err = fmt.Errorf("Expected Restore Phase %s, got %s:%s", phase, restore.Status.Phase, restore.Status.Reason)
	}
	return
}

// ignoreRestoreError returns nil if the Restore failure can safely be ignored for the given Operand
func ignoreRestoreError(csv string, operand *version.Operand, ispn *ispnv1.Infinispan, restore *v2.Restore, client tutils.HTTPClient, err error) error {
	fmt.Println(err)
	switch restore.Status.Phase {
	case v2.RestoreInitializing:
	case v2.RestoreInitialized:
		// Ignore errors where the Restore state is stuck initializing due to a MERGED cluster view from PublishNotReadyAddresses=false
		// Fixed in Operator 2.3.2, but ISPN-14793 should fix this for Operands >= 14.0.9 even with PublishNotReadyAddresses=false
		operatorVersion := semver.MustParse(strings.Split(csv, "v")[1])
		if operatorVersion.LT(semver.Version{Major: 2, Minor: 3, Patch: 2}) {
			return nil
		}
		fallthrough
	case v2.RestoreRunning:
	case v2.RestoreSucceeded:
	case v2.RestoreUnknown:
		return err
	}

	// Ignore split brain errors on older Operands that are caused by one of the cluster members
	// restarting due to an incompatible configuration caused by ISPN-15089 and ISPN-15191
	if operand.UpstreamVersion.LT(semver.Version{Major: 14, Minor: 0, Patch: 17}) &&
		strings.Contains(restore.Status.Reason, "Key 'ClusteredLockKey{name=BackupManagerImpl-restore}' is not available. Not all owners are in this partition") {
		// Check the restartCount and logs of the cluster pods to ensure ISPN-15089 is the cause of
		// a server pod restarting
		podList := &corev1.PodList{}
		tutils.ExpectNoError(
			testKube.Kubernetes.ResourcesList(restore.Namespace, map[string]string{"app": "infinispan-pod", "infinispan_cr": ispn.Name}, podList, ctx),
		)

		r := regexp.MustCompile("FATAL.*ISPN080028.*GlobalConfigurationManager failed to start")
		for _, pod := range podList.Items {
			for _, c := range pod.Status.ContainerStatuses {
				if c.Name == provision.InfinispanContainer && c.RestartCount > 0 {
					logs, err := testKube.Kubernetes.Logs(pod.Name, pod.Namespace, true, ctx)
					tutils.ExpectNoError(err)

					if r.MatchString(logs) {
						fmt.Printf("Ignoring Restore failure caused by ISPN-15089 on Operand %s\n", operand)
						// Force the org.infinispan.LOCKS cache to become available so that subsequent upgrades will proceed
						tutils.NewCacheHelper("org.infinispan.LOCKS", client).Available(true)
						return nil
					}
				}
			}
		}
	}

	if strings.Contains(restore.Status.Reason, "EOF") {
		logs, err := testKube.Kubernetes.Logs(restore.Name, restore.Namespace, false, ctx)
		tutils.ExpectNoError(err)

		if operand.UpstreamVersion.LT(semver.Version{Major: 14, Minor: 0, Patch: 18}) {
			// Ignore ISPN-15181 related errors on older Operands
			r := regexp.MustCompile(`Error executing command PutKeyValueCommand on Cache 'org.infinispan.CONFIG'.*org.infinispan.util.concurrent.TimeoutException: ISPN000476`)
			if r.MatchString(logs) {
				fmt.Printf("Ignoring Restore failure caused by ISPN-15181 on Operand %s\n", operand)
				return nil
			}
		}

		if operand.UpstreamVersion.EQ(semver.Version{Major: 14, Minor: 0, Patch: 9}) {
			// Ignore example.PROTOBUF_DIST incompatibility. Users can workaround this issue by excluding the example.PROTOBUF_DIST
			// template from their Restore CR
			r := regexp.MustCompile(`ISPN000961: Incompatible attribute 'media-type.example.PROTOBUF_DIST.distributed-cache-configuration.encoding'`)
			if r.MatchString(logs) {
				fmt.Printf("Ignoring Restore failure caused by example.PROTOBUF_DIST template encoding incompatibility %s\n", operand)
				return nil
			}
		}
	}
	return err
}

func checkServicePorts(t *testing.T, name string) {
	// Check two services are exposed and the admin service listens on its own port
	userPort := testKube.GetServicePorts(tutils.Namespace, name)
	assert.Equal(t, 1, len(userPort))
	assert.Equal(t, constants.InfinispanUserPort, int(userPort[0].Port))

	adminPort := testKube.GetAdminServicePorts(tutils.Namespace, name)
	assert.Equal(t, 1, len(adminPort))
	assert.Equal(t, constants.InfinispanAdminPort, int(adminPort[0].Port))
}

func checkBatch(t *testing.T, name string) {
	// Run a batch in the migrated cluster
	batchHelper := batchtest.NewBatchHelper(testKube)
	config := "create cache --template=org.infinispan.DIST_SYNC batch-cache"
	batchHelper.CreateBatch(t, name, name, &config, nil)
	batchHelper.WaitForValidBatchPhase(name, v2.BatchSucceeded)
}
