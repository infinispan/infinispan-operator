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

const (
	persistentCacheName = "persistentCache"
	volatileCacheName   = "volatileCache"
)

var (
	conditionTimeout = 2 * tutils.ConditionWaitTimeout
	ctx              = context.TODO()
	testKube         = tutils.NewTestKubernetes(os.Getenv("TESTING_CONTEXT"))
)

func subscription(olm tutils.OLMEnv) *coreos.Subscription {
	return &coreos.Subscription{
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
}

func createAndPopulateVolatileCache(cacheName string, numEntries int, client tutils.HTTPClient) {
	c := tutils.NewCacheHelper(cacheName, client)
	if !c.Exists() {
		c.Create(`{"distributed-cache":{"mode":"SYNC"}}`, mime.ApplicationJson)
	}
	if c.Size() != numEntries {
		c.Populate(numEntries)
		c.AssertSize(numEntries)
	}
}

func createAndPopulatePersistentCache(cacheName string, numEntries int, client tutils.HTTPClient) {
	cache := tutils.NewCacheHelper(cacheName, client)
	config := `{"distributed-cache":{"mode":"SYNC", "persistence":{"file-store":{}}}}`
	cache.Create(config, mime.ApplicationJson)
	cache.Populate(numEntries)
	cache.AssertSize(numEntries)
}

func createBackupAndWaitToSucceed(name string, t *testing.T) *v2.Backup {
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
			Cluster: name,
			Resources: &v2.BackupResources{
				Caches:       []string{volatileCacheName},
				CacheConfigs: []string{},
				Templates:    []string{},
				ProtoSchemas: []string{},
			},
		},
	}
	testKube.Create(backup)
	return testKube.WaitForValidBackupPhase(backup.Name, backup.Namespace, v2.BackupSucceeded)
}

func createRestoreAndWaitToSucceed(name string, backup *v2.Backup, t *testing.T) (*v2.Restore, error) {
	restore := &v2.Restore{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v2alpha1",
			Kind:       "Restore",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: tutils.Namespace,
			Labels:    map[string]string{"test-name": t.Name()},
		},
		Spec: v2.RestoreSpec{
			Backup:  backup.Name,
			Cluster: backup.Spec.Cluster,
		},
	}
	testKube.Create(restore)
	return waitForValidRestorePhase(restore.Name, restore.Namespace, v2.RestoreSucceeded)
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
	tutils.Log().Info(err)
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
					logs, err := testKube.Kubernetes.Logs(provision.InfinispanContainer, pod.Name, pod.Namespace, true, ctx)
					tutils.ExpectNoError(err)

					if r.MatchString(logs) {
						tutils.Log().Infof("Ignoring Restore failure caused by ISPN-15089 on Operand %s", operand)
						// Force the org.infinispan.LOCKS cache to become available so that subsequent upgrades will proceed
						tutils.NewCacheHelper("org.infinispan.LOCKS", client).Available(true)
						return nil
					}
				}
			}
		}
	}

	if strings.Contains(restore.Status.Reason, "EOF") {
		logs, err := testKube.Kubernetes.Logs(provision.InfinispanContainer, restore.Name, restore.Namespace, false, ctx)
		tutils.ExpectNoError(err)

		if operand.UpstreamVersion.LT(semver.Version{Major: 14, Minor: 0, Patch: 18}) {
			// Ignore ISPN-15181 related errors on older Operands
			r := regexp.MustCompile(`Error executing command PutKeyValueCommand on Cache 'org.infinispan.CONFIG'.*org.infinispan.util.concurrent.TimeoutException: ISPN000476`)
			if r.MatchString(logs) {
				tutils.Log().Infof("Ignoring Restore failure caused by ISPN-15181 on Operand %s", operand)
				return nil
			}
		}

		if operand.UpstreamVersion.EQ(semver.Version{Major: 14, Minor: 0, Patch: 9}) {
			// Ignore example.PROTOBUF_DIST incompatibility. Users can workaround this issue by excluding the example.PROTOBUF_DIST
			// template from their Restore CR
			r := regexp.MustCompile(`ISPN000961: Incompatible attribute 'media-type.example.PROTOBUF_DIST.distributed-cache-configuration.encoding'`)
			if r.MatchString(logs) {
				tutils.Log().Infof("Ignoring Restore failure caused by example.PROTOBUF_DIST template encoding incompatibility %s", operand)
				return nil
			}
		}
	}

	if strings.Contains(restore.Status.Reason, "unable to retrieve Restore with name") {
		logs, err := testKube.Kubernetes.Logs(provision.InfinispanContainer, restore.Name, restore.Namespace, false, ctx)
		tutils.ExpectNoError(err)

		// Ignore https://github.com/infinispan/infinispan/issues/13571
		r := regexp.MustCompile(`ISPN000436: Cache '.*.' has been requested, but no matching cache configuration exists`)
		if r.MatchString(logs) {
			tutils.Log().Infof("Ignoring Restore failure caused by https://github.com/infinispan/infinispan/issues/13571 on Operand %s", operand)
			return nil
		}
	}
	return err
}

func assertOperandImage(expectedImage string, i *ispnv1.Infinispan) {
	pods := &corev1.PodList{}
	err := testKube.Kubernetes.ResourcesList(tutils.Namespace, i.PodSelectorLabels(), pods, ctx)
	tutils.ExpectNoError(err)
	for _, pod := range pods.Items {
		if pod.Spec.Containers[0].Image != expectedImage {
			panic(fmt.Errorf("upgraded image [%v] in Pod not equal desired cluster image [%v]", pod.Spec.Containers[0].Image, expectedImage))
		}
	}
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

func checkBatch(t *testing.T, infinispan *ispnv1.Infinispan) {
	// Run a batch in the migrated cluster
	batchHelper := batchtest.NewBatchHelper(testKube)
	configMap := batchHelper.CreateBatchCM(infinispan)
	defer testKube.DeleteConfigMap(configMap)

	batch := batchHelper.CreateBatch(t, infinispan.Name, infinispan.Name, nil, &(configMap.Name), nil)
	batchHelper.WaitForValidBatchPhase(infinispan.Name, v2.BatchSucceeded)
	testKube.DeleteBatch(batch)
}

func assertNoDegradedCaches() {
	podList := &corev1.PodList{}
	tutils.ExpectNoError(
		testKube.Kubernetes.ResourcesList(tutils.Namespace, map[string]string{"app": "infinispan-pod"}, podList, ctx),
	)

	for _, pod := range podList.Items {
		log, err := testKube.Kubernetes.Logs(provision.InfinispanContainer, pod.GetName(), pod.GetNamespace(), false, ctx)
		tutils.ExpectNoError(err)
		if strings.Contains(log, "DEGRADED_MODE") {
			panic("Detected a cache in DEGRADED_MODE, unable to continue with test")
		}
	}
}
