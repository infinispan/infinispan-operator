package hotrod_rolling_upgrade

import (
	"fmt"
	"os"
	"strconv"
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	"github.com/stretchr/testify/assert"
)

var testKube = tutils.NewTestKubernetes(os.Getenv("TESTING_CONTEXT"))

func TestMain(m *testing.M) {
	tutils.RunOperator(m, testKube)
}

func TestRollingUpgrade(t *testing.T) {
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	numPods := 2
	entriesPerCache := 100
	// Create a cluster with the oldest supported Operand release
	infinispan := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		operandSrc := tutils.VersionManager.Operands[0]
		i.Spec.Version = operandSrc.Ref()
		i.Spec.Replicas = int32(numPods)
		i.Spec.Container.CPU = "1000m"
		i.Spec.Service.Container.EphemeralStorage = false
		i.Spec.Expose = &ispnv1.ExposeSpec{
			Type:     ispnv1.ExposeTypeNodePort,
			NodePort: 30000,
		}
		i.Spec.Upgrades = &ispnv1.InfinispanUpgradesSpec{
			Type: ispnv1.UpgradeTypeHotRodRolling,
		}
	})

	testKube.Create(infinispan)
	testKube.WaitForInfinispanPods(numPods, tutils.SinglePodTimeout, infinispan.Name, tutils.Namespace)
	infinispan = testKube.WaitForInfinispanCondition(infinispan.Name, tutils.Namespace, ispnv1.ConditionWellFormed)
	client := tutils.HTTPClientForCluster(infinispan, testKube)

	// Create caches
	createCache("textCache", mime.TextPlain, client)
	createCache("jsonCache", mime.ApplicationJson, client)
	createCache("javaCache", mime.ApplicationJavaObject, client)
	createCache("indexedCache", mime.ApplicationProtostream, client)

	// Add data to some caches
	addData("textCache", entriesPerCache, client)
	addData("indexedCache", entriesPerCache, client)

	// Upgrade to the next available Operand version
	tutils.ExpectNoError(
		testKube.UpdateInfinispan(infinispan, func() {
			operandDst := tutils.VersionManager.Operands[1]
			infinispan.Spec.Version = operandDst.Ref()
		}),
	)

	// Check migration
	currentStatefulSetName := infinispan.Name
	newStatefulSetName := infinispan.Name + "-1"

	testKube.WaitForStateFulSet(newStatefulSetName, tutils.Namespace)
	testKube.WaitForStateFulSetRemoval(currentStatefulSetName, tutils.Namespace)
	testKube.WaitForInfinispanPodsCreatedBy(numPods, tutils.SinglePodTimeout, newStatefulSetName, tutils.Namespace)
	testKube.WaitForInfinispanPodsCreatedBy(0, tutils.SinglePodTimeout, currentStatefulSetName, tutils.Namespace)

	if !tutils.CheckExternalAddress(client) {
		panic("Error contacting server")
	}

	// Check data
	assert.Equal(t, entriesPerCache, cacheSize("textCache", client))
	assert.Equal(t, entriesPerCache, cacheSize("indexedCache", client))
	assert.Equal(t, 0, cacheSize("jsonCache", client))
	assert.Equal(t, 0, cacheSize("javaCache", client))
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
	fmt.Printf("Populated cache %s with %d entries", cacheName, entries)
}
