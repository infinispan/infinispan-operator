package hotrod_rolling_upgrade

import (
	"context"
	"fmt"
	"os"
	"strconv"
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
	ctx              = context.TODO()
	conditionTimeout = 2 * tutils.ConditionWaitTimeout
	testKube         = tutils.NewTestKubernetes(os.Getenv("TESTING_CONTEXT"))
)

func TestMain(t *testing.M) {
	code := t.Run()
	os.Exit(code)
}

func TestRollingUpgrade(t *testing.T) {
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
	})
	// Explicitly reset the Version so that it will be set by the Operator webhook
	spec.Spec.Version = ""

	testKube.Create(spec)
	testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	spec = testKube.WaitForInfinispanCondition(spec.Name, tutils.Namespace, ispnv1.ConditionWellFormed)
	versionManager := testKube.VersionManagerFromCSV(sub)
	client := tutils.HTTPClientForClusterWithVersionManager(spec, testKube, versionManager)

	// Create caches
	createCache("textCache", mime.TextPlain, client)
	createCache("jsonCache", mime.ApplicationJson, client)
	createCache("javaCache", mime.ApplicationJavaObject, client)

	// Add data to some caches
	addData("textCache", entriesPerCache, client)

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
		time.Sleep(time.Minute)

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
			assert.Equal(t, entriesPerCache, cacheSize("textCache", client))
			assert.Equal(t, 0, cacheSize("jsonCache", client))
			assert.Equal(t, 0, cacheSize("javaCache", client))

			if indexedSupported {
				assert.Equal(t, entriesPerCache, cacheSize(IndexedCacheName, client))
			}
		}

		if ispnPreUpgrade.Spec.Version == "" {
			relatedImageJdk := testKube.InstalledCSVEnv("RELATED_IMAGE_OPENJDK", sub)
			if relatedImageJdk != "" {
				// The latest Operator version still doesn't support multi-operand so check that the RELATED_IMAGE_OPENJDK
				// image has been installed on all pods
				assertMigration(relatedImageJdk, false, false)
				continue
			}

			// This is the first upgrade to an Operator with multi-operand support, so wait for the oldest Operand
			oldestOperand := versionManager.Oldest()
			ispnPreUpgrade = testKube.WaitForInfinispanState(spec.Name, spec.Namespace, func(i *ispnv1.Infinispan) bool {
				return i.IsConditionTrue(ispnv1.ConditionWellFormed) &&
					i.Status.Operand.Version == oldestOperand.Ref() &&
					i.Status.Operand.Image == oldestOperand.Image &&
					i.Status.Operand.Phase == ispnv1.OperandPhaseRunning
			})
			pods := &corev1.PodList{}
			err := testKube.Kubernetes.ResourcesList(tutils.Namespace, spec.PodSelectorLabels(), pods, ctx)
			tutils.ExpectNoError(err)
			for _, pod := range pods.Items {
				if pod.Spec.Containers[0].Image != oldestOperand.Image {
					panic(fmt.Errorf("upgraded image [%v] in Pod not equal desired cluster image [%v]", pod.Spec.Containers[0].Image, oldestOperand.Image))
				}
			}
		}

		latestOperand := versionManager.Latest()
		currentOperand, err := versionManager.WithRef(ispnPreUpgrade.Spec.Version)
		client = tutils.HTTPClientForClusterWithVersionManager(spec, testKube, versionManager)
		tutils.ExpectNoError(err)
		if !currentOperand.EQ(latestOperand) {
			ispn := testKube.WaitForInfinispanConditionWithTimeout(spec.Name, tutils.Namespace, ispnv1.ConditionWellFormed, conditionTimeout)

			// ISPN-15651 Test migrating Indexed caches from 14.0.25.Final onwards
			indexSupported := latestOperand.UpstreamVersion.GTE(*version.Operand{UpstreamVersion: &semver.Version{Major: 14, Minor: 0, Patch: 25}}.UpstreamVersion)
			if indexSupported {
				createIndexedCache(entriesPerCache, client)
			}

			skippedOperands := tutils.OperandSkipSet()
			if _, skip := skippedOperands[latestOperand.Ref()]; skip {
				// Skip Operand upgrade if explicitly ignored
				log.Infof("Skipping Operand %s", latestOperand.Ref())
				continue
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
	tutils.Log().Infof("Populated cache %s with %d entries", cacheName, entries)
}

func createIndexedCache(entries int, client tutils.HTTPClient) {
	cache := tutils.NewCacheHelper(IndexedCacheName, client)
	if cache.Exists() {
		tutils.Log().Infof("Cache '%s' already exists", IndexedCacheName)
		return
	}
	proto := `
package book_sample;

/* @Indexed */
message Book {
	/* @Field(store = Store.YES, analyze = Analyze.YES) */
	/* @Text(projectable = true) */
	optional string title = 1;

	/* @Text(projectable = true) */
	optional string description = 2;

	// no native Date type available in Protobuf
	optional int32 publicationYear = 3;

	repeated Author authors = 4;
}

message Author {
	optional string name = 1;
	optional string surname = 2;
}
`
	headers := map[string]string{
		"Content-Type": "application/x-www-form-urlencoded",
	}
	_, err := client.Post("rest/v2/caches/___protobuf_metadata/schema.proto", proto, headers)
	tutils.ExpectNoError(err)

	config := "{\"distributed-cache\":{\"encoding\":{\"media-type\":\"application/x-protostream\"},\"persistence\":{\"file-store\":{}},\"indexing\":{\"indexed-entities\":[\"book_sample.Book\"]}}}"
	cache.Create(config, mime.ApplicationJson)
	for i := 0; i < entries; i++ {
		data := fmt.Sprintf("{\"_type\":\"book_sample.Book\",\"title\":\"book%d\"}", i)
		cache.Put(data, data, mime.ApplicationJson)
	}
	tutils.Log().Infof("Populated cache %s with %d entries", IndexedCacheName, entries)
}
