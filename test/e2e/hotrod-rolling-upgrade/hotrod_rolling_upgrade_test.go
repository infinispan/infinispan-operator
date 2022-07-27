package hotrod_rolling_upgrade

import (
	"fmt"
	"os"
	"strconv"
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	coreos "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	entriesPerCache = 100
	numPods         = 2
	clusterName     = "demo-cluster"
)

var testKube = tutils.NewTestKubernetes(os.Getenv("TESTING_CONTEXT"))

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

	defer testKube.CleanupOLMTest(t, olm.SubName, olm.SubNamespace, olm.SubPackage)
	testKube.CreateSubscriptionAndApproveInitialVersion(olm, sub)

	infinispan := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
		i.Name = clusterName
		i.Spec.Replicas = int32(numPods)
		i.Spec.Container.CPU = "1000m"
		i.Spec.Service.Container.EphemeralStorage = false
		i.Spec.Upgrades = &ispnv1.InfinispanUpgradesSpec{
			Type: ispnv1.UpgradeTypeHotRodRolling,
		}
	})

	testKube.Create(infinispan)
	testKube.WaitForInfinispanPods(numPods, tutils.SinglePodTimeout, clusterName, tutils.Namespace)
	testKube.WaitForInfinispanCondition(clusterName, tutils.Namespace, ispnv1.ConditionWellFormed)
	client := tutils.HTTPClientForCluster(infinispan, testKube)

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
	newStatefulSetName := clusterName

	// Approve InstallPlans and verify cluster state on each upgrade until the most recent CSV has been reached
	for testKube.Subscription(sub); sub.Status.InstalledCSV != targetChannel.CurrentCSVName; {
		fmt.Printf("Installed csv: %s, Current CSV: %s\n", sub.Status.InstalledCSV, targetChannel.CurrentCSVName)
		testKube.WaitForSubscriptionState(coreos.SubscriptionStateUpgradePending, sub)
		testKube.ApproveInstallPlan(sub)

		testKube.WaitForSubscription(sub, func() bool {
			return sub.Status.InstalledCSV == sub.Status.CurrentCSV
		})

		// Check migration
		clusterCounter++
		currentStatefulSetName := newStatefulSetName
		newStatefulSetName := fmt.Sprintf("%s-%d", clusterName, clusterCounter)

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
