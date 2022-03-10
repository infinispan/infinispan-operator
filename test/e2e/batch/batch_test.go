package batch

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	v1 "github.com/infinispan/infinispan-operator/api/v1"
	v2 "github.com/infinispan/infinispan-operator/api/v2alpha1"
	batchCtrl "github.com/infinispan/infinispan-operator/controllers"
	ispnClient "github.com/infinispan/infinispan-operator/pkg/infinispan/client"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
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

	helper.WaitForValidBatchPhase(name, v2.BatchSucceeded)

	httpClient := tutils.HTTPClientForCluster(infinispan, testKube)
	ispn := ispnClient.New(httpClient)
	assertCacheExists("batch-cache", ispn)
	assertCounterExists("batch-counter", ispn)
	testKube.DeleteBatch(batch)
	waitForK8sResourceCleanup(name)
}

func TestBatchConfigMap(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	infinispan := createCluster(t)

	configMapName := infinispan.Name + "-cm"
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

	batch := helper.CreateBatch(t, infinispan.Name, infinispan.Name, nil, &configMapName)

	helper.WaitForValidBatchPhase(infinispan.Name, v2.BatchSucceeded)
	testKube.DeleteBatch(batch)
	waitForK8sResourceCleanup(infinispan.Name)

	httpClient := tutils.HTTPClientForCluster(infinispan, testKube)
	ispn := ispnClient.New(httpClient)
	assertCacheExists("batch-cache", ispn)
	assertCounterExists("batch-counter", ispn)
}

func TestBatchFail(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	infinispan := createCluster(t)

	batchScript := "SOME INVALID BATCH CMD!"
	batch := helper.CreateBatch(t, infinispan.Name, infinispan.Name, &batchScript, nil)

	helper.WaitForValidBatchPhase(infinispan.Name, v2.BatchFailed)
	testKube.DeleteBatch(batch)
	waitForK8sResourceCleanup(infinispan.Name)
}

func batchString() string {
	batchScript := `create cache --template=org.infinispan.DIST_SYNC batch-cache
	create counter --concurrency-level=1 --initial-value=5 --storage=VOLATILE --type=weak batch-counter`
	return strings.ReplaceAll(batchScript, "\t", "")
}

func createCluster(t *testing.T) *v1.Infinispan {
	infinispan := tutils.DefaultSpec(t, testKube)
	testKube.Create(infinispan)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, infinispan.Name, tutils.Namespace)
	return infinispan
}

func waitForK8sResourceCleanup(name string) {
	// Ensure that the created Job has completed and has been removed
	err := wait.Poll(10*time.Millisecond, tutils.TestTimeout, func() (bool, error) {
		return !testKube.AssertK8ResourceExists(name, tutils.Namespace, &batchv1.Job{}), nil
	})
	tutils.ExpectNoError(err)

	// If no Job pods available, then the pods have been garbage collected
	err = wait.Poll(tutils.DefaultPollPeriod, tutils.TestTimeout, func() (bool, error) {
		_, e := batchCtrl.GetJobPodName(name, tutils.Namespace, testKube.Kubernetes.Client, ctx)
		return e != nil, nil
	})
	tutils.ExpectNoError(err)
}

func assertCacheExists(cacheName string, i api.Infinispan) {
	exists, err := i.Cache(cacheName).Exists()
	tutils.ExpectNoError(err)
	if !exists {
		panic(fmt.Sprintf("Caches %s does not exist", cacheName))
	}
}

func assertCounterExists(cacheName string, i api.Infinispan) {
	// TODO once Counters added to API
	// exists, err :=
	// tutils.ExpectNoError(err)
	// if !exists {
	// 	panic(fmt.Sprintf("Caches %s does not exist", cacheName))
	// }
}
