package upgrade

import (
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iancoleman/strcase"
	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/api/v2alpha1"
	"github.com/infinispan/infinispan-operator/controllers"
	"github.com/infinispan/infinispan-operator/controllers/constants"
	batchtest "github.com/infinispan/infinispan-operator/test/e2e/batch"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	"github.com/operator-framework/api/pkg/manifests"
	coreosv1 "github.com/operator-framework/api/pkg/operators/v1"
	coreos "github.com/operator-framework/api/pkg/operators/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
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
	sourceChannel   = getChannel("SUBSCRIPTION_CHANNEL_SOURCE", 1, packageManifest)
	targetChannel   = getChannel("SUBSCRIPTION_CHANNEL_TARGET", 0, packageManifest)

	subStartingCsv = constants.GetEnvWithDefault("SUBSCRIPTION_STARTING_CSV", sourceChannel.CurrentCSVName)

	conditionTimeout = 2 * tutils.ConditionWaitTimeout
)

func TestUpgrade(t *testing.T) {
	printManifest(t)
	name := strcase.ToKebab(t.Name())
	labels := map[string]string{"test-name": name}

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

	defer cleanup(t, labels)
	testKube.CreateOperatorGroup(t, subName, tutils.Namespace, tutils.Namespace)
	testKube.CreateSubscription(t, sub)

	// Approve the initial startingCSV InstallPlan
	testKube.WaitForSubscriptionState(t, coreos.SubscriptionStateUpgradePending, sub)
	testKube.ApproveInstallPlan(t, sub)

	testKube.WaitForCrd(&apiextv1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: "infinispans.infinispan.org",
		},
	})

	// Create the Infinispan CR
	replicas := 2
	spec := tutils.DefaultSpec(t, testKube)
	spec.Name = name
	spec.Labels = labels
	spec.Spec.Replicas = int32(replicas)
	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(t, replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	testKube.WaitForInfinispanConditionWithTimeout(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed, conditionTimeout)

	// Add a persistent cache with data to ensure contents can be read after upgrade(s)
	numEntries := 100
	hostAddr, client := tutils.HTTPClientAndHost(spec, testKube)
	cacheName := "someCache"
	populateCache(t, cacheName, hostAddr, numEntries, spec, client)
	assertNumEntries(t, cacheName, hostAddr, numEntries, client)

	// Upgrade the Subscription channel if required
	if sourceChannel != targetChannel {
		testKube.UpdateSubscriptionChannel(t, targetChannel.Name, sub)
	}

	// Approve InstallPlans and verify cluster state on each upgrade until the most recent CSV has been reached
	for testKube.Subscription(t, sub); sub.Status.InstalledCSV != targetChannel.CurrentCSVName; {
		testKube.WaitForSubscriptionState(t, coreos.SubscriptionStateUpgradePending, sub)
		testKube.ApproveInstallPlan(t, sub)

		testKube.WaitForSubscription(t, sub, func() bool {
			return sub.Status.InstalledCSV == sub.Status.CurrentCSV
		})

		// Ensure that the cluster is shutting down
		testKube.WaitForInfinispanConditionWithTimeout(name, tutils.Namespace, ispnv1.ConditionStopping, conditionTimeout)
		testKube.WaitForInfinispanPods(t, replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
		testKube.WaitForInfinispanConditionWithTimeout(name, tutils.Namespace, ispnv1.ConditionWellFormed, conditionTimeout)

		// Validates that all pods are running with desired image
		expectedImage := testKube.InstalledCSVServerImage(t, sub)
		pods := &corev1.PodList{}
		err := testKube.Kubernetes.ResourcesList(tutils.Namespace, controllers.PodLabels(spec.Name), pods, ctx)
		require.NoError(t, err)
		for _, pod := range pods.Items {
			require.EqualValues(t, pod.Spec.Containers[0].Image, expectedImage, "upgraded image [%v] in Pod not equal desired cluster image [%v]", pod.Spec.Containers[0].Image, expectedImage)
		}

		// Ensure that persistent cache entries have survived the upgrade(s)
		// Refresh the hostAddr and client as the url will change if NodePort is used.
		hostAddr, client = tutils.HTTPClientAndHost(spec, testKube)
		assertNumEntries(t, cacheName, hostAddr, numEntries, client)
	}

	checkServicePorts(t, name)
	checkBatch(t, name)

	// Kill the first pod to ensure that the cluster can recover from failover after upgrade
	err := testKube.Kubernetes.Client.Delete(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-0",
			Namespace: tutils.Namespace,
		},
	})
	require.NoError(t, err)
	testKube.WaitForInfinispanPods(t, replicas, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	testKube.WaitForInfinispanConditionWithTimeout(name, tutils.Namespace, ispnv1.ConditionWellFormed, conditionTimeout)

	// Ensure that persistent cache entries still contain the expected numEntries
	hostAddr, client = tutils.HTTPClientAndHost(spec, testKube)
	assertNumEntries(t, cacheName, hostAddr, numEntries, client)
}

func cleanup(t *testing.T, specLabel map[string]string) {
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
			testKube.PrintAllResources(subNamespace, &coreosv1.OperatorGroupList{}, map[string]string{})
			testKube.PrintAllResources(subNamespace, &coreos.SubscriptionList{}, map[string]string{})
			testKube.PrintAllResources(subNamespace, &coreos.ClusterServiceVersionList{}, map[string]string{})
			// Print 2.1.x Operator pod logs
			testKube.PrintAllResources(subNamespace, &corev1.PodList{}, map[string]string{"name": "infinispan-operator"})
			// Print latest Operator logs
			testKube.PrintAllResources(subNamespace, &corev1.PodList{}, map[string]string{"app.kubernetes.io/name": "infinispan-operator"})
		}

		// Cleanup OLM resources
		testKube.DeleteSubscription(t, subName, subNamespace)
		testKube.DeleteOperatorGroup(t, subName, subNamespace)
		tutils.ExpectMaybeNotFound(t, testKube.Kubernetes.Client.DeleteAllOf(ctx, &coreos.ClusterServiceVersion{}, opts...))
	}
	// We must cleanup OLM resources after any Infinispan CRs etc, otherwise the CRDs may have been removed from the cluster
	defer cleanupOlm()

	// Cleanup Infinispan resources
	testKube.CleanNamespaceAndLogWithPanic(t, tutils.Namespace, specLabel, panicVal)
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
	batchHelper.WaitForValidBatchPhase(t, name, v2alpha1.BatchSucceeded)
}

// Utilise the provided env variable for the channel string if it exists, otherwise retrieve the channel from the PackageManifest
func getChannel(env string, stackPos int, manifest *manifests.PackageManifest) manifests.PackageChannel {
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

	semVerIndex := -1
	for i := len(channels) - 1; i >= 0; i-- {
		// Hack to find the first channel that uses semantic versioning names. Required for upstream, as old non-sem versioned channels exist, e.g. "stable", "alpha".
		if strings.ContainsAny(channels[i].Name, ".") {
			semVerIndex = i
			break
		}
	}
	return channels[semVerIndex-stackPos]
}

func printManifest(t *testing.T) {
	bytes, err := yaml.Marshal(packageManifest)
	require.NoError(t, err)
	fmt.Println(string(bytes))
	fmt.Println("Source channel: " + sourceChannel.Name)
	fmt.Println("Target channel: " + targetChannel.Name)
	fmt.Println("Starting CSV: " + subStartingCsv)
}

func populateCache(t *testing.T, cacheName, host string, numEntries int, infinispan *ispnv1.Infinispan, client tutils.HTTPClient) {
	post := func(url, payload string, status int, headers map[string]string) {
		rsp, err := client.Post(url, payload, headers)
		require.NoError(t, err)
		defer tutils.CloseHttpResponse(t, rsp)
		require.Equal(t, status, rsp.StatusCode, "Unexpected response code %d", rsp.StatusCode)
	}

	headers := map[string]string{"Content-Type": "application/json"}
	url := fmt.Sprintf("%s/rest/v2/caches/%s", host, cacheName)
	config := `{"distributed-cache":{"mode":"SYNC", "persistence":{"file-store":{}}}}`
	post(url, config, http.StatusOK, headers)

	for i := 0; i < numEntries; i++ {
		url := fmt.Sprintf("%s/rest/v2/caches/%s/%d", host, cacheName, i)
		value := fmt.Sprintf("{\"value\":\"%d\"}", i)
		post(url, value, http.StatusNoContent, headers)
	}
}

func assertNumEntries(t *testing.T, cacheName, host string, expectedEntries int, client tutils.HTTPClient) {
	url := fmt.Sprintf("%s/rest/v2/caches/%s?action=size", host, cacheName)
	var entries int
	err := wait.Poll(tutils.DefaultPollPeriod, 30*time.Second, func() (done bool, err error) {
		rsp, err := client.Get(url, nil)
		require.NoError(t, err)
		if rsp.StatusCode != http.StatusOK {
			body, _ := ioutil.ReadAll(rsp.Body)
			assert.FailNow(t, "Unexpected response code %d, body='%s'", rsp.StatusCode, body)
		}

		body, err := ioutil.ReadAll(rsp.Body)
		require.NoError(t, rsp.Body.Close())
		require.NoError(t, err)
		numRead, err := strconv.ParseInt(string(body), 10, 64)
		entries = int(numRead)
		return entries == expectedEntries, err
	})
	if err != nil {
		url := fmt.Sprintf("%s/rest/v2/caches/%s?action=config", host, cacheName)
		rsp, err := client.Get(url, map[string]string{"accept": "application/yaml"})
		if err == nil {
			body, err := ioutil.ReadAll(rsp.Body)
			require.NoError(t, rsp.Body.Close())
			require.NoError(t, err)
			fmt.Printf("%s Config:\n%s", cacheName, string(body))
		} else {
			fmt.Printf("Encountered error when trying to retrieve '%s' config: %v", cacheName, err)
		}
		assert.FailNow(t, "Expected %d entries found %d: %w", expectedEntries, entries, err)
	}
}
