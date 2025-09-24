package infinispan

import (
	"fmt"
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	"github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan/handler/provision"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

func TestOnlyShutdownCluster(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	// Cache definitions
	volatileCache := "volatile-cache"
	volatileCacheConfig := `<distributed-cache name="` + volatileCache + `"/>`
	filestoreCache := "filestore-cache"
	filestoreCacheConfig := `<distributed-cache name ="` + filestoreCache + `"><persistence><file-store/></persistence></distributed-cache>`

	// Create Infinispan
	replicas := 2
	spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Replicas = int32(replicas)
		i.Spec.Service.Container.EphemeralStorage = false
		i.Spec.ConfigListener.Enabled = true
	})
	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)

	ispn := testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)
	testKube.WaitForDeployment(ispn.GetConfigListenerName(), spec.Namespace)
	client_ := tutils.HTTPClientForCluster(ispn, testKube)

	// Create non-persisted cache
	volatileCacheHelper := tutils.NewCacheHelper(volatileCache, client_)
	volatileCacheHelper.Create(volatileCacheConfig, mime.ApplicationXml)

	// Create persisted cache with an entry
	filestoreKey := "testFilestoreKey"
	filestoreValue := "testFilestoreValue"

	filestoreCacheHelper := tutils.NewCacheHelper(filestoreCache, client_)
	filestoreCacheHelper.Create(filestoreCacheConfig, mime.ApplicationXml)
	filestoreCacheHelper.Put(filestoreKey, filestoreValue, mime.TextPlain)

	// Shutdown the cluster
	testKube.GracefulShutdownInfinispan(spec)
	testKube.WaitForInfinispanPods(0, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	testKube.WaitForDeploymentState(spec.GetConfigListenerName(), spec.Namespace, func(deployment *appsv1.Deployment) bool {
		return deployment.Spec.Replicas != nil && *deployment.Spec.Replicas == 0
	})
}

// TestGracefulShutdownWithTwoReplicas creates a permanent cache with file-store and any entry,
// shutdowns the cluster and checks that the cache and the data are still there
func TestGracefulShutdownWithTwoReplicas(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	// Cache definitions
	volatileCache := "volatile-cache"
	volatileCacheConfig := `<distributed-cache name="` + volatileCache + `"/>`
	filestoreCache := "filestore-cache"
	filestoreCacheConfig := `<distributed-cache name ="` + filestoreCache + `"><persistence><file-store/></persistence></distributed-cache>`

	// Create Infinispan
	replicas := 2
	spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Replicas = int32(replicas)
		i.Spec.Service.Container.EphemeralStorage = false
		i.Spec.ConfigListener.Enabled = true
	})
	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)

	ispn := testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)
	testKube.WaitForDeployment(ispn.GetConfigListenerName(), spec.Namespace)
	client_ := tutils.HTTPClientForCluster(ispn, testKube)

	// Create non-persisted cache
	volatileCacheHelper := tutils.NewCacheHelper(volatileCache, client_)
	volatileCacheHelper.Create(volatileCacheConfig, mime.ApplicationXml)

	// Create persisted cache with an entry
	filestoreKey := "testFilestoreKey"
	filestoreValue := "testFilestoreValue"

	filestoreCacheHelper := tutils.NewCacheHelper(filestoreCache, client_)
	filestoreCacheHelper.Create(filestoreCacheConfig, mime.ApplicationXml)
	filestoreCacheHelper.Put(filestoreKey, filestoreValue, mime.TextPlain)

	// Shutdown/bring back the cluster
	testKube.GracefulShutdownInfinispan(spec)
	testKube.WaitForInfinispanPods(0, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	testKube.WaitForDeploymentState(spec.GetConfigListenerName(), spec.Namespace, func(deployment *appsv1.Deployment) bool {
		return deployment.Spec.Replicas != nil && *deployment.Spec.Replicas == 0
	})
   t.Fatalf("Cluster was shutdown, mark as error to get logs.")
	//testKube.GracefulRestartInfinispan(spec, int32(replicas), tutils.SinglePodTimeout)
	//testKube.WaitForDeploymentState(spec.GetConfigListenerName(), spec.Namespace, func(deployment *appsv1.Deployment) bool {
	//	return deployment.Spec.Replicas != nil && *deployment.Spec.Replicas == 1
	//})
	// Verify non-persisted cache usability
	//volatileKey := "volatileKey"
	//volatileValue := "volatileValue"

	// Attempt to put entry right after the cluster went back online may result in EOF sometimes due to request not reaching the cluster
	//volatileCacheHelper.TestBasicUsageWithRetry(volatileKey, volatileValue, 5)

	//volatileCacheHelper.Delete()

	// Verify persisted cache usability and data presence
	//actual, _ := filestoreCacheHelper.Get(filestoreKey)
	//if actual != filestoreValue {
	//	panic(fmt.Errorf("unexpected actual returned: %v (value %v)", actual, filestoreValue))
	//}

	//filestoreCacheHelper.Delete()
}

func TestScaling(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	ispn := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Spec.Service.Container.EphemeralStorage = false
	})
	testKube.CreateInfinispan(ispn, tutils.Namespace)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, ispn.Name, tutils.Namespace)
	ispn = testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, ispnv1.ConditionWellFormed)

	// Test scaling up
	tutils.ExpectNoError(
		testKube.UpdateInfinispan(ispn, func() {
			ispn.Spec.Replicas = 2
		}),
	)
	testKube.WaitForInfinispanPods(2, tutils.SinglePodTimeout, ispn.Name, tutils.Namespace)

	// Test scaling down
	ispn = testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, ispnv1.ConditionWellFormed)
	tutils.ExpectNoError(
		testKube.UpdateInfinispan(ispn, func() {
			ispn.Spec.Replicas = 1
		}),
	)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, ispn.Name, tutils.Namespace)
	var pvcs *corev1.PersistentVolumeClaimList
	tutils.ExpectNoError(
		wait.Poll(tutils.DefaultPollPeriod, tutils.ConditionWaitTimeout, func() (bool, error) {
			pvcs = testKube.GetPVCList(tutils.Namespace, ispn.PodSelectorLabels())
			return len(pvcs.Items) == 1, nil
		}),
	)
	assert.Equal(t, fmt.Sprintf("%s-%s-0", provision.DataMountVolume, ispn.GetStatefulSetName()), pvcs.Items[0].Name)
}
