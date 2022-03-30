package infinispan

import (
	"fmt"
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
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
	})
	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)

	ispn := testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)
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
	testKube.GracefulRestartInfinispan(spec, int32(replicas), tutils.SinglePodTimeout)

	// Verify non-persisted cache usability
	volatileKey := "volatileKey"
	volatileValue := "volatileValue"

	volatileCacheHelper.TestBasicUsage(volatileKey, volatileValue)

	// [ISPN-13740] Server may return 500 upon deleting cache after graceful shutdown of a cluster
	// defaultCacheHelper.Delete()

	// Verify persisted cache usability and data presence
	actual, _ := filestoreCacheHelper.Get(filestoreKey)
	if actual != filestoreValue {
		panic(fmt.Errorf("unexpected actual returned: %v (value %v)", actual, filestoreValue))
	}

	// [ISPN-13740] Server may return 500 upon deleting cache after graceful shutdown of a cluster
	// filestoreCacheHelper.Delete()
}
