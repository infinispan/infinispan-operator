package infinispan

import (
	"fmt"
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	appsv1 "k8s.io/api/apps/v1"
)

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
	testKube.GracefulRestartInfinispan(spec, int32(replicas), tutils.SinglePodTimeout)
	testKube.WaitForDeploymentState(spec.GetConfigListenerName(), spec.Namespace, func(deployment *appsv1.Deployment) bool {
		return deployment.Spec.Replicas != nil && *deployment.Spec.Replicas == 1
	})
	// Verify non-persisted cache usability
	volatileKey := "volatileKey"
	volatileValue := "volatileValue"

	volatileCacheHelper.TestBasicUsage(volatileKey, volatileValue)

	volatileCacheHelper.Delete()

	// Verify persisted cache usability and data presence
	actual, _ := filestoreCacheHelper.Get(filestoreKey)
	if actual != filestoreValue {
		panic(fmt.Errorf("unexpected actual returned: %v (value %v)", actual, filestoreValue))
	}

	filestoreCacheHelper.Delete()
}
