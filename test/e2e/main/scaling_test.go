package main

import (
	"fmt"
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
)

// TestPermanentCache creates a permanent cache the stop/start
// the cluster and checks that the cache is still there
func TestPermanentCache(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	cacheName := "test"
	// Define function for the generic stop/start test procedure
	var createPermanentCache = func(ispn *ispnv1.Infinispan) {
		client_ := tutils.HTTPClientForCluster(ispn, testKube)
		tutils.NewCacheHelper(cacheName, client_).CreateWithDefault("PERMANENT")
	}

	var usePermanentCache = func(ispn *ispnv1.Infinispan) {
		client_ := tutils.HTTPClientForCluster(ispn, testKube)
		key := "test"
		value := "test-operator"
		cacheHelper := tutils.NewCacheHelper(cacheName, client_)
		cacheHelper.TestBasicUsage(key, value)
		cacheHelper.Delete()
	}

	genericTestForGracefulShutdown(t, createPermanentCache, usePermanentCache)
}

// TestCheckDataSurviveToShutdown creates a cache with file-store the stop/start
// the cluster and checks that the cache and the data are still there
func TestCheckDataSurviveToShutdown(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	cacheName := "test"
	template := `<infinispan><cache-container><distributed-cache name ="` + cacheName +
		`"><persistence><file-store/></persistence></distributed-cache></cache-container></infinispan>`
	key := "test"
	value := "test-operator"

	// Define function for the generic stop/start test procedure
	var createCacheWithFileStore = func(ispn *ispnv1.Infinispan) {
		client_ := tutils.HTTPClientForCluster(ispn, testKube)
		cacheHelper := tutils.NewCacheHelper(cacheName, client_)
		cacheHelper.Create(template, mime.ApplicationXml)
		cacheHelper.Put(key, value, mime.TextPlain)
	}

	var useCacheWithFileStore = func(ispn *ispnv1.Infinispan) {
		client_ := tutils.HTTPClientForCluster(ispn, testKube)
		cacheHelper := tutils.NewCacheHelper(cacheName, client_)
		actual, _ := cacheHelper.Get(key)
		if actual != value {
			panic(fmt.Errorf("unexpected actual returned: %v (value %v)", actual, value))
		}
		cacheHelper.Delete()
	}

	genericTestForGracefulShutdown(t, createCacheWithFileStore, useCacheWithFileStore)
}

func genericTestForGracefulShutdown(t *testing.T, modifier func(*ispnv1.Infinispan), verifier func(*ispnv1.Infinispan)) {
	// Create a resource without passing any config
	// Register it
	spec := tutils.DefaultSpec(t, testKube)
	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	ispn := testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	// Do something that needs to be permanent
	modifier(ispn)

	// Delete the cluster
	testKube.GracefulShutdownInfinispan(spec)
	testKube.GracefulRestartInfinispan(spec, 1, tutils.SinglePodTimeout)

	// Do something that checks that permanent changes are there again
	verifier(ispn)
}
