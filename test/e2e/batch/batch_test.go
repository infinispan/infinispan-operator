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
	"github.com/stretchr/testify/require"
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
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	infinispan := createCluster(t)
	testBatchInlineConfig(t, infinispan)
}

func testBatchInlineConfig(t *testing.T, infinispan *v1.Infinispan) {
	name := infinispan.Name
	batchScript := batchString()
	batch := helper.CreateBatch(t, name, name, &batchScript, nil)

	helper.WaitForValidBatchPhase(t, name, v2.BatchSucceeded)

	client := httpClient(t, infinispan)
	hostAddr := hostAddr(t, client, infinispan)
	assertRestOk(t, cachesURL("batch-cache", hostAddr), client)
	assertRestOk(t, countersURL("batch-counter", hostAddr), client)
	testKube.DeleteBatch(batch)
	waitForK8sResourceCleanup(t, name)
}

func TestBatchConfigMap(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	name := strcase.ToKebab(t.Name())
	infinispan := createCluster(t)

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

	testKube.CreateConfigMap(configMap)
	defer testKube.DeleteConfigMap(configMap)

	batch := helper.CreateBatch(t, name, name, nil, &configMapName)

	helper.WaitForValidBatchPhase(t, name, v2.BatchSucceeded)
	testKube.DeleteBatch(batch)
	waitForK8sResourceCleanup(t, name)

	client := httpClient(t, infinispan)
	hostAddr := hostAddr(t, client, infinispan)
	assertRestOk(t, cachesURL("batch-cache", hostAddr), client)
	assertRestOk(t, countersURL("batch-counter", hostAddr), client)
}

func TestBatchNoConfigOrConfigMap(t *testing.T) {
	t.Parallel()
	name := strcase.ToKebab(t.Name())
	helper.CreateBatch(t, name, "doesn't exist", nil, nil)

	batch := helper.WaitForValidBatchPhase(t, name, v2.BatchFailed)
	require.Equal(t, "'Spec.config' OR 'spec.ConfigMap' must be configured", batch.Status.Reason, "Unexpected 'Status.Reason': %s", batch.Status.Reason)
	testKube.DeleteBatch(batch)
	waitForK8sResourceCleanup(t, name)
}

func TestBatchConfigAndConfigMap(t *testing.T) {
	t.Parallel()
	name := strcase.ToKebab(t.Name())
	helper.CreateBatch(t, name, "doesn't exist", pointer.StringPtr("Config"), pointer.StringPtr("ConfigMap"))

	batch := helper.WaitForValidBatchPhase(t, name, v2.BatchFailed)
	require.Equal(t, "at most one of ['Spec.config', 'spec.ConfigMap'] must be configured", batch.Status.Reason, "Unexpected 'Status.Reason': %s", batch.Status.Reason)
	testKube.DeleteBatch(batch)
	waitForK8sResourceCleanup(t, name)
}

func TestBatchFail(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	infinispan := createCluster(t)
	name := infinispan.Name

	batchScript := "SOME INVALID BATCH CMD!"
	batch := helper.CreateBatch(t, name, name, &batchScript, nil)

	helper.WaitForValidBatchPhase(t, name, v2.BatchFailed)
	testKube.DeleteBatch(batch)
	waitForK8sResourceCleanup(t, name)
}

func batchString() string {
	batchScript := `create cache --template=org.infinispan.DIST_SYNC batch-cache
	create counter --concurrency-level=1 --initial-value=5 --storage=VOLATILE --type=weak batch-counter`
	return strings.ReplaceAll(batchScript, "\t", "")
}

func httpClient(t *testing.T, infinispan *v1.Infinispan) tutils.HTTPClient {
	user := consts.DefaultDeveloperUser
	password, err := users.UserPassword(user, infinispan.GetSecretName(), tutils.Namespace, testKube.Kubernetes, context.TODO())
	require.NoError(t, err)
	protocol := testKube.GetSchemaForRest(infinispan)
	return tutils.NewHTTPClient(user, password, protocol)
}

func cachesURL(cacheName, hostAddr string) string {
	return fmt.Sprintf("%v/rest/v2/caches/%s", hostAddr, cacheName)
}

func countersURL(counterName, hostAddr string) string {
	return fmt.Sprintf("%v/rest/v2/counters/%s", hostAddr, counterName)
}

func hostAddr(t *testing.T, client tutils.HTTPClient, infinispan *v1.Infinispan) string {
	return testKube.WaitForExternalService(infinispan, tutils.RouteTimeout, client)
}

func createCluster(t *testing.T) *v1.Infinispan {
	infinispan := tutils.DefaultSpec(t, testKube)
	testKube.Create(infinispan)
	testKube.WaitForInfinispanPods(t, 1, tutils.SinglePodTimeout, infinispan.Name, tutils.Namespace)
	return infinispan
}

func waitForK8sResourceCleanup(t *testing.T, name string) {
	// Ensure that the created Job has completed and has been removed
	err := wait.Poll(10*time.Millisecond, tutils.TestTimeout, func() (bool, error) {
		return !assertK8ResourceExists(name, &batchv1.Job{}), nil
	})
	require.NoError(t, err)

	// If no Job pods available, then the pods have been garbage collected
	err = wait.Poll(tutils.DefaultPollPeriod, tutils.TestTimeout, func() (bool, error) {
		_, e := batchCtrl.GetJobPodName(name, tutils.Namespace, testKube.Kubernetes.Client, ctx)
		return e != nil, nil
	})
	require.NoError(t, err)
}

func assertK8ResourceExists(name string, obj client.Object) bool {
	client := testKube.Kubernetes.Client
	key := types.NamespacedName{
		Name:      name,
		Namespace: tutils.Namespace,
	}
	return client.Get(ctx, key, obj) == nil
}

func assertRestOk(t *testing.T, url string, client tutils.HTTPClient) {
	rsp, err := client.Get(url, nil)
	require.NoError(t, err)
	defer tutils.CloseHttpResponse(t, rsp)
	require.Equal(t, http.StatusOK, rsp.StatusCode, "Expected Status Code 200, received %d", rsp.StatusCode)
}
