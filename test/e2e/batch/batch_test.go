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
	v1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	v2 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v2alpha1"
	batchCtrl "github.com/infinispan/infinispan-operator/pkg/controller/batch"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	users "github.com/infinispan/infinispan-operator/pkg/infinispan/security"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/pointer"
)

var (
	ctx      = context.Background()
	testKube = tutils.NewTestKubernetes(os.Getenv("TESTING_CONTEXT"))
)

func TestMain(m *testing.M) {
	tutils.RunOperator(m, testKube)
}

func TestBatchInlineConfig(t *testing.T) {
	t.Parallel()
	name := strcase.ToKebab(t.Name())
	infinispan := createCluster(name)
	defer testKube.DeleteInfinispan(infinispan, tutils.SinglePodTimeout)
	testBatchInlineConfig(infinispan)
}

func TestBatchInlineConfigWithTLS(t *testing.T) {
	t.Parallel()
	infinispan := tutils.DefaultSpec(testKube)
	infinispan.ObjectMeta.Name = strcase.ToKebab(t.Name())
	infinispan.Spec.Security = v1.InfinispanSecurity{
		EndpointEncryption: &tutils.EndpointEncryption,
	}

	// Create secret
	testKube.CreateSecret(&tutils.EncryptionSecret, tutils.Namespace)
	defer testKube.DeleteSecret(&tutils.EncryptionSecret)

	testKube.Create(infinispan)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, infinispan.Name, tutils.Namespace)
	defer testKube.DeleteInfinispan(infinispan, tutils.SinglePodTimeout)
	testBatchInlineConfig(infinispan)
}

func testBatchInlineConfig(infinispan *v1.Infinispan) {
	name := infinispan.Name
	batchScript := batchString()
	batch := createBatch(name, name, &batchScript, nil)

	waitForValidBatchPhase(name, v2.BatchSucceeded)

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
	defer testKube.DeleteInfinispan(infinispan, tutils.SinglePodTimeout)

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

	batch := createBatch(name, name, nil, &configMapName)

	waitForValidBatchPhase(name, v2.BatchSucceeded)
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
	batch := createBatch(name, "doesn't exist", nil, nil)

	batch = waitForValidBatchPhase(name, v2.BatchFailed)
	if batch.Status.Reason != "'Spec.config' OR 'spec.ConfigMap' must be configured" {
		panic(fmt.Errorf("Unexpected 'Status.Reason': %s", batch.Status.Reason))
	}
	testKube.DeleteBatch(batch)
	waitForK8sResourceCleanup(name)
}

func TestBatchConfigAndConfigMap(t *testing.T) {
	t.Parallel()
	name := strcase.ToKebab(t.Name())
	batch := createBatch(name, "doesn't exist", pointer.StringPtr("Config"), pointer.StringPtr("ConfigMap"))

	batch = waitForValidBatchPhase(name, v2.BatchFailed)
	if batch.Status.Reason != "At most one of ['Spec.config', 'spec.ConfigMap'] must be configured" {
		panic(fmt.Errorf("Unexpected 'Status.Reason': %s", batch.Status.Reason))
	}
	testKube.DeleteBatch(batch)
	waitForK8sResourceCleanup(name)
}

func TestBatchFail(t *testing.T) {
	t.Parallel()
	name := strcase.ToKebab(t.Name())
	infinispan := createCluster(name)
	defer testKube.DeleteInfinispan(infinispan, tutils.SinglePodTimeout)

	batchScript := "SOME INVALID BATCH CMD!"
	batch := createBatch(name, name, &batchScript, nil)

	waitForValidBatchPhase(name, v2.BatchFailed)
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
	password, err := users.UserPassword(user, infinispan.GetSecretName(), tutils.Namespace, testKube.Kubernetes)
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
	routeName := fmt.Sprintf("%s-external", infinispan.Name)
	return testKube.WaitForExternalService(routeName, tutils.Namespace, infinispan.GetExposeType(), tutils.RouteTimeout, client)
}

func createCluster(name string) *v1.Infinispan {
	infinispan := tutils.DefaultSpec(testKube)
	infinispan.Name = name
	testKube.Create(infinispan)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, infinispan.Name, tutils.Namespace)
	return infinispan
}

func createBatch(name, cluster string, config, configMap *string) *v2.Batch {
	batch := &v2.Batch{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v2alpha1",
			Kind:       "Batch",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: tutils.Namespace,
		},
		Spec: v2.BatchSpec{
			Cluster:   cluster,
			Config:    config,
			ConfigMap: configMap,
		},
	}
	testKube.Create(batch)
	return batch
}

func waitForValidBatchPhase(name string, phase v2.BatchPhase) *v2.Batch {
	var batch *v2.Batch
	err := wait.Poll(10*time.Millisecond, tutils.TestTimeout, func() (bool, error) {
		batch = testKube.GetBatch(name, tutils.Namespace)
		if batch.Status.Phase == v2.BatchFailed && phase != v2.BatchFailed {
			return true, fmt.Errorf("Batch failed. Reason: %s", batch.Status.Reason)
		}
		return phase == batch.Status.Phase, nil
	})
	if err != nil {
		println(fmt.Sprintf("Expected Batch Phase %s, got %s:%s", phase, batch.Status.Phase, batch.Status.Reason))
	}
	tutils.ExpectNoError(err)
	return batch
}

func waitForK8sResourceCleanup(name string) {
	// Ensure that the created Job has completed and has been removed
	err := wait.Poll(10*time.Millisecond, tutils.TestTimeout, func() (bool, error) {
		return !assertK8ResourceExists(name, &batchv1.Job{}), nil
	})
	tutils.ExpectNoError(err)

	// If no Job pods available, then the pods have been garbage collected
	err = wait.Poll(tutils.DefaultPollPeriod, tutils.TestTimeout, func() (bool, error) {
		_, e := batchCtrl.GetJobPodName(name, tutils.Namespace, testKube.Kubernetes.Client)
		return e != nil, nil
	})
	tutils.ExpectNoError(err)
}

func assertK8ResourceExists(name string, obj runtime.Object) bool {
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
	if rsp.StatusCode != http.StatusOK {
		panic(fmt.Sprintf("Expected Status Code 200, received %d", rsp.StatusCode))
	}
}
