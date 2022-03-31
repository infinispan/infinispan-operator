package upgrade

import (
	"context"
	"fmt"
	"os"
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/api/v2alpha1"
	"github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	batchtest "github.com/infinispan/infinispan-operator/test/e2e/batch"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	"github.com/operator-framework/api/pkg/manifests"
	coreosv1 "github.com/operator-framework/api/pkg/operators/v1"
	coreos "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	ctx                   = context.TODO()
	testKube              = tutils.NewTestKubernetes(os.Getenv("TESTING_CONTEXT"))
	subName               = constants.GetEnvWithDefault("SUBSCRIPTION_NAME", "infinispan-operator")
	subNamespace          = constants.GetEnvWithDefault("SUBSCRIPTION_NAMESPACE", tutils.Namespace)
	catalogSource         = constants.GetEnvWithDefault("SUBSCRIPTION_CATALOG_SOURCE", "test-catalog")
	catalogSourcNamespace = constants.GetEnvWithDefault("SUBSCRIPTION_CATALOG_SOURCE_NAMESPACE", tutils.Namespace)
	subPackage            = constants.GetEnvWithDefault("SUBSCRIPTION_PACKAGE", "infinispan")

	packageManifest = testKube.PackageManifest(subPackage, catalogSource)
	sourceChannel   = getChannel("SUBSCRIPTION_CHANNEL_SOURCE", packageManifest)
	targetChannel   = getChannel("SUBSCRIPTION_CHANNEL_TARGET", packageManifest)

	subStartingCsv = constants.GetEnvWithDefault("SUBSCRIPTION_STARTING_CSV", sourceChannel.CurrentCSVName)

	conditionTimeout = 2 * tutils.ConditionWaitTimeout
)

func TestUpgrade(t *testing.T) {
	printManifest()

	testKube.NewNamespace(tutils.Namespace)
	sub := &coreos.Subscription{
		TypeMeta: metav1.TypeMeta{
			APIVersion: coreos.SubscriptionCRDAPIVersion,
			Kind:       coreos.SubscriptionKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      subName,
			Namespace: subNamespace,
		},
		Spec: &coreos.SubscriptionSpec{
			Channel:                sourceChannel.Name,
			CatalogSource:          catalogSource,
			CatalogSourceNamespace: catalogSourcNamespace,
			InstallPlanApproval:    coreos.ApprovalManual,
			Package:                subPackage,
			StartingCSV:            subStartingCsv,
		},
	}

	defer cleanup(t)
	// Create OperatorGroup only if Subscription is created in non-global namespace
	if subNamespace != "openshift-operators" && subNamespace != "operators" {
		testKube.CreateOperatorGroup(subName, subNamespace, subNamespace)
	}
	testKube.CreateSubscription(sub)

	// Approve the initial startingCSV InstallPlan
	testKube.WaitForSubscriptionState(coreos.SubscriptionStateUpgradePending, sub)
	testKube.ApproveInstallPlan(sub)

	testKube.WaitForCrd(&apiextv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "infinispans.infinispan.org",
		},
	})

	// Create the Infinispan CR
	replicas := 2
	spec := tutils.DefaultSpec(t, testKube)
	spec.Spec.Replicas = int32(replicas)
	spec.Spec.Service.Container.EphemeralStorage = false
	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	testKube.WaitForInfinispanConditionWithTimeout(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed, conditionTimeout)

	// Add a persistent cache with data to ensure contents can be read after upgrade(s)
	numEntries := 100
	client := tutils.HTTPClientForCluster(spec, testKube)
	cacheName := "someCache"

	cache := tutils.NewCacheHelper(cacheName, client)
	config := `{"distributed-cache":{"mode":"SYNC", "persistence":{"file-store":{}}}}`
	cache.Create(config, mime.ApplicationJson)
	cache.Populate(numEntries)
	cache.AssertSize(numEntries)

	// Upgrade the Subscription channel if required
	if sourceChannel != targetChannel {
		testKube.UpdateSubscriptionChannel(targetChannel.Name, sub)
	}

	// Approve InstallPlans and verify cluster state on each upgrade until the most recent CSV has been reached
	for testKube.Subscription(sub); sub.Status.InstalledCSV != targetChannel.CurrentCSVName; {
		fmt.Printf("Installed csv: %s, Current CSV: %s", sub.Status.InstalledCSV, targetChannel.CurrentCSVName)
		testKube.WaitForSubscriptionState(coreos.SubscriptionStateUpgradePending, sub)
		testKube.ApproveInstallPlan(sub)

		testKube.WaitForSubscription(sub, func() bool {
			return sub.Status.InstalledCSV == sub.Status.CurrentCSV
		})

		// Ensure that the cluster is shutting down
		testKube.WaitForInfinispanConditionWithTimeout(spec.Name, tutils.Namespace, ispnv1.ConditionStopping, conditionTimeout)
		testKube.WaitForInfinispanPods(replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
		testKube.WaitForInfinispanConditionWithTimeout(spec.Name, tutils.Namespace, ispnv1.ConditionWellFormed, conditionTimeout)

		// Validates that all pods are running with desired image
		expectedImage := testKube.InstalledCSVServerImage(sub)
		pods := &corev1.PodList{}
		err := testKube.Kubernetes.ResourcesList(tutils.Namespace, spec.PodLabels(), pods, ctx)
		tutils.ExpectNoError(err)
		for _, pod := range pods.Items {
			if pod.Spec.Containers[0].Image != expectedImage {
				tutils.ExpectNoError(fmt.Errorf("upgraded image [%v] in Pod not equal desired cluster image [%v]", pod.Spec.Containers[0].Image, expectedImage))
			}
		}

		// Ensure that persistent cache entries have survived the upgrade(s)
		// Refresh the hostAddr and client as the url will change if NodePort is used.
		client = tutils.HTTPClientForCluster(spec, testKube)
		tutils.NewCacheHelper(cacheName, client).AssertSize(numEntries)
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
	tutils.NewCacheHelper(cacheName, client).AssertSize(numEntries)
}

func cleanup(t *testing.T) {
	panicVal := recover()

	cleanupOlm := func() {
		opts := []client.DeleteAllOfOption{
			client.InNamespace(subNamespace),
			client.MatchingLabels(
				map[string]string{
					fmt.Sprintf("operators.coreos.com/%s.%s", subPackage, subNamespace): "",
				},
			),
		}

		if panicVal != nil {
			dir := os.TempDir() + "/infinispan-operator/" + tutils.TestName(t)

			testKube.PrintAllResources(dir, subNamespace, "OperatorGroup", &coreosv1.OperatorGroupList{}, map[string]string{})
			testKube.PrintAllResources(dir, subNamespace, "Subscription", &coreos.SubscriptionList{}, map[string]string{})
			testKube.PrintAllResources(dir, subNamespace, "ClusterServiceVersion", &coreos.ClusterServiceVersionList{}, map[string]string{})
			// Print 2.1.x Operator pod logs
			testKube.PrintAllResources(dir, subNamespace, "Pod", &corev1.PodList{}, map[string]string{"name": "infinispan-operator"})
			// Print latest Operator logs
			testKube.PrintAllResources(dir, subNamespace, "Pod", &corev1.PodList{}, map[string]string{"app.kubernetes.io/name": "infinispan-operator"})
		}

		// Cleanup OLM resources
		testKube.DeleteSubscription(subName, subNamespace)
		testKube.DeleteOperatorGroup(subName, subNamespace)
		tutils.ExpectMaybeNotFound(testKube.Kubernetes.Client.DeleteAllOf(ctx, &coreos.ClusterServiceVersion{}, opts...))
	}
	// We must cleanup OLM resources after any Infinispan CRs etc, otherwise the CRDs may have been removed from the cluster
	defer cleanupOlm()

	// Cleanup Infinispan resources
	testKube.CleanNamespaceAndLogWithPanic(t, tutils.Namespace, panicVal)
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
	batchHelper.WaitForValidBatchPhase(name, v2alpha1.BatchSucceeded)
}

// Utilise the provided env variable for the channel string if it exists, otherwise retrieve the channel from the PackageManifest
func getChannel(env string, manifest *manifests.PackageManifest) manifests.PackageChannel {
	channels := manifest.Channels

	// If an env variable exists, then return the PackageChannel with that name
	if env, exists := os.LookupEnv(env); exists {
		for _, channel := range channels {
			if channel.Name == env {
				return channel
			}
		}
		panic(fmt.Errorf("unable to find channel with name '%s' in PackageManifest", env))
	}

	for _, channel := range channels {
		if channel.Name == manifest.DefaultChannelName {
			return channel
		}
	}
	return manifests.PackageChannel{}
}

func printManifest() {
	bytes, err := yaml.Marshal(packageManifest)
	tutils.ExpectNoError(err)
	fmt.Println(string(bytes))
	fmt.Println("Source channel: " + sourceChannel.Name)
	fmt.Println("Target channel: " + targetChannel.Name)
	fmt.Println("Starting CSV: " + subStartingCsv)
}
