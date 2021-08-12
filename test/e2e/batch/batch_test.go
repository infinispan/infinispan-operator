package batch

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/iancoleman/strcase"
	v1 "github.com/infinispan/infinispan-operator/api/v1"
	v2 "github.com/infinispan/infinispan-operator/api/v2alpha1"
	batchCtrl "github.com/infinispan/infinispan-operator/controllers"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	users "github.com/infinispan/infinispan-operator/pkg/infinispan/security"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	ctx      = context.Background()
	testKube = tutils.NewTestKubernetes(os.Getenv("TESTING_CONTEXT"))
	helper   = NewBatchHelper(testKube)
)

func TestMain(m *testing.M) {
	tutils.RunOperator(m, testKube)
}

func TestBatchInlineConfig(t *testing.T) {
	t.Parallel()
	name := strcase.ToKebab(t.Name())
	infinispan := createCluster(name)
	defer testKube.CleanNamespaceAndLogOnPanic(tutils.Namespace, infinispan.Labels)
	testBatchInlineConfig(infinispan)
}

func testBatchInlineConfig(infinispan *v1.Infinispan) {
	name := infinispan.Name
	batchScript := batchString()
	batch := helper.CreateBatch(name, name, &batchScript, nil)

	helper.WaitForValidBatchPhase(name, v2.BatchSucceeded)

	client := httpClient(infinispan)
	hostAddr := hostAddr(client, infinispan)
	assertRestOk(cachesURL("batch-cache", hostAddr), client)
	assertRestOk(countersURL("batch-counter", hostAddr), client)
	testKube.DeleteBatch(batch)
	waitForK8sResourceCleanup(name)
}

func TestBatchConfigMap(t *testing.T) {
	t.Parallel()
	name := strcase.ToKebab(t.Name())
	infinispan := createCluster(name)
	defer testKube.CleanNamespaceAndLogOnPanic(tutils.Namespace, infinispan.Labels)

	configMapName := name + "-cm"
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: infinispan.Namespace,
		},
		Data: map[string]string{
			batchCtrl.BatchFilename: batchString(),
		},
	}

	if err := testKube.Kubernetes.Client.Create(ctx, configMap); err != nil {
		panic(fmt.Errorf("Unable to create ConfigMap: %w", err))
	}

	batch := helper.CreateBatch(name, name, nil, &configMapName)

	helper.WaitForValidBatchPhase(name, v2.BatchSucceeded)
	testKube.DeleteBatch(batch)
	waitForK8sResourceCleanup(name)

	client := httpClient(infinispan)
	hostAddr := hostAddr(client, infinispan)
	assertRestOk(cachesURL("batch-cache", hostAddr), client)
	assertRestOk(countersURL("batch-counter", hostAddr), client)
}

func TestBatchNoConfigOrConfigMap(t *testing.T) {
	t.Parallel()
	name := strcase.ToKebab(t.Name())
	helper.CreateBatch(name, "doesn't exist", nil, nil)

	batch := helper.WaitForValidBatchPhase(name, v2.BatchFailed)
	if batch.Status.Reason != "'Spec.config' OR 'spec.ConfigMap' must be configured" {
		panic(fmt.Errorf("Unexpected 'Status.Reason': %s", batch.Status.Reason))
	}
	testKube.DeleteBatch(batch)
	waitForK8sResourceCleanup(name)
}

func TestBatchConfigAndConfigMap(t *testing.T) {
	t.Parallel()
	name := strcase.ToKebab(t.Name())
	helper.CreateBatch(name, "doesn't exist", pointer.StringPtr("Config"), pointer.StringPtr("ConfigMap"))

	batch := helper.WaitForValidBatchPhase(name, v2.BatchFailed)
	if batch.Status.Reason != "at most one of ['Spec.config', 'spec.ConfigMap'] must be configured" {
		panic(fmt.Errorf("Unexpected 'Status.Reason': %s", batch.Status.Reason))
	}
	testKube.DeleteBatch(batch)
	waitForK8sResourceCleanup(name)
}

func TestBatchFail(t *testing.T) {
	t.Parallel()
	name := strcase.ToKebab(t.Name())
	infinispan := createCluster(name)
	defer testKube.CleanNamespaceAndLogOnPanic(tutils.Namespace, infinispan.Labels)

	batchScript := "SOME INVALID BATCH CMD!"
	batch := helper.CreateBatch(name, name, &batchScript, nil)

	helper.WaitForValidBatchPhase(name, v2.BatchFailed)
	testKube.DeleteBatch(batch)
	waitForK8sResourceCleanup(name)
}

func batchString() string {
	batchScript := `create cache --template=org.infinispan.DIST_SYNC batch-cache
	create counter --concurrency-level=1 --initial-value=5 --storage=VOLATILE --type=weak batch-counter`
	return strings.ReplaceAll(batchScript, "\t", "")
}

func httpClient(infinispan *v1.Infinispan) tutils.HTTPClient {
	user := consts.DefaultDeveloperUser
	password, err := users.UserPassword(user, infinispan.GetSecretName(), tutils.Namespace, testKube.Kubernetes, context.TODO())
	tutils.ExpectNoError(err)
	protocol := testKube.GetSchemaForRest(infinispan)
	return tutils.NewHTTPClient(user, password, protocol)
}

func cachesURL(cacheName, hostAddr string) string {
	return fmt.Sprintf("%v/rest/v2/caches/%s", hostAddr, cacheName)
}

func countersURL(counterName, hostAddr string) string {
	return fmt.Sprintf("%v/rest/v2/counters/%s", hostAddr, counterName)
}

func hostAddr(client tutils.HTTPClient, infinispan *v1.Infinispan) string {
	return testKube.WaitForExternalService(infinispan, tutils.RouteTimeout, client)
}

func createCluster(name string) *v1.Infinispan {
	infinispan := tutils.DefaultSpec(testKube)
	infinispan.Name = name
	infinispan.Labels = map[string]string{"test-name": name}
	testKube.Create(infinispan)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, infinispan.Name, tutils.Namespace)
	return infinispan
}

func waitForK8sResourceCleanup(name string) {
	// Ensure that the created Job has completed and has been removed
	err := wait.Poll(10*time.Millisecond, tutils.TestTimeout, func() (bool, error) {
		return !assertK8ResourceExists(name, &batchv1.Job{}), nil
	})
	tutils.ExpectNoError(err)

	// If no Job pods available, then the pods have been garbage collected
	err = wait.Poll(tutils.DefaultPollPeriod, tutils.TestTimeout, func() (bool, error) {
		_, e := batchCtrl.GetJobPodName(name, tutils.Namespace, testKube.Kubernetes.Client, ctx)
		return e != nil, nil
	})
	tutils.ExpectNoError(err)
}

func assertK8ResourceExists(name string, obj client.Object) bool {
	client := testKube.Kubernetes.Client
	key := types.NamespacedName{
		Name:      name,
		Namespace: tutils.Namespace,
	}
	return client.Get(ctx, key, obj) == nil
}

func assertRestOk(url string, client tutils.HTTPClient) {
	rsp, err := client.Get(url, nil)
	tutils.ExpectNoError(err)
	defer tutils.CloseHttpResponse(rsp)
	if rsp.StatusCode != http.StatusOK {
		panic(fmt.Sprintf("Expected Status Code 200, received %d", rsp.StatusCode))
	}
}
