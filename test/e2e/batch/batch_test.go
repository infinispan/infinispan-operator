package batch

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/zapr"
	v1 "github.com/infinispan/infinispan-operator/api/v1"
	v2 "github.com/infinispan/infinispan-operator/api/v2alpha1"
	batchCtrl "github.com/infinispan/infinispan-operator/controllers"
	ispnClient "github.com/infinispan/infinispan-operator/pkg/infinispan/client"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	"github.com/stretchr/testify/assert"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	testKube = tutils.NewTestKubernetes(os.Getenv("TESTING_CONTEXT"))
	helper   = NewBatchHelper(testKube)
)

func TestMain(m *testing.M) {
	ctrl.SetLogger(zapr.NewLogger(tutils.Log().Desugar()))
	tutils.RunOperator(m, testKube)
}

func TestBatchInlineConfig(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	infinispan := createCluster(t, nil)
	testBatchInlineConfig(t, infinispan)
}

func testBatchInlineConfig(t *testing.T, infinispan *v1.Infinispan) {
	name := infinispan.Name
	batchScript := batchString()
	batch := helper.CreateBatch(t, name, name, &batchScript, nil, nil)

	helper.WaitForValidBatchPhase(name, v2.BatchSucceeded)

	httpClient := tutils.HTTPClientForCluster(infinispan, testKube)
	ispn := ispnClient.New(tutils.CurrentOperand, httpClient)
	assertCounterExists("batch-counter", ispn)
	testKube.DeleteBatch(batch)
	waitForK8sResourceCleanup(name)
}

func TestBatchConfigMap(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	infinispan := createCluster(t, nil)
	configMap := helper.CreateBatchCM(infinispan)
	defer testKube.DeleteConfigMap(configMap)

	batch := helper.CreateBatch(t, infinispan.Name, infinispan.Name, nil, &(configMap.Name), nil)

	helper.WaitForValidBatchPhase(infinispan.Name, v2.BatchSucceeded)
	testKube.DeleteBatch(batch)
	waitForK8sResourceCleanup(infinispan.Name)

	httpClient := tutils.HTTPClientForCluster(infinispan, testKube)
	ispn := ispnClient.New(tutils.CurrentOperand, httpClient)
	assertCacheExists("mycache", ispn)
}

func TestBatchFail(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	infinispan := createCluster(t, nil)

	batchScript := "SOME INVALID BATCH CMD!"
	batch := helper.CreateBatch(t, infinispan.Name, infinispan.Name, &batchScript, nil, nil)

	helper.WaitForValidBatchPhase(infinispan.Name, v2.BatchFailed)
	testKube.DeleteBatch(batch)
	waitForK8sResourceCleanup(infinispan.Name)
}

func TestBatchWithResources(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	infinispan := createCluster(t, nil)
	batchScript := batchString()
	bcSpec := &v2.BatchContainerSpec{Memory: "1Gi:1Gi", CPU: "500m:500m"}
	podRes := batchCtrl.BatchResources(bcSpec)
	batch := helper.CreateBatch(t, infinispan.Name, infinispan.Name, &batchScript, nil, bcSpec)

	helper.WaitForValidBatchPhase(infinispan.Name, v2.BatchRunning)

	job := testKube.GetJob(infinispan.Name, tutils.Namespace)
	limits := job.Spec.Template.Spec.Containers[0].Resources.Limits
	requests := job.Spec.Template.Spec.Containers[0].Resources.Requests
	if !limits.Cpu().Equal(*podRes.Limits.Cpu()) ||
		!limits.Memory().Equal(*podRes.Limits.Memory()) ||
		!requests.Cpu().Equal(*podRes.Requests.Cpu()) ||
		!requests.Memory().Equal(*podRes.Requests.Memory()) {
		panic(fmt.Errorf("unexpected error"))
	}
	testKube.DeleteBatch(batch)
	waitForK8sResourceCleanup(infinispan.Name)
}

// TestBatchSecurityContext verifies that the Batch Job pod and container inherit the target
// Infinispan cluster's securityContext.
func TestBatchSecurityContext(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	// Configure the cluster's securityContext; the Batch pod must inherit it.
	infinispan := createCluster(t, func(i *v1.Infinispan) {
		i.Spec.SecurityContext = &corev1.PodSecurityContext{SupplementalGroups: []int64{1000}}
		i.Spec.Container.SecurityContext = &corev1.SecurityContext{ReadOnlyRootFilesystem: ptr.To(false)}
	})

	batchScript := batchString()
	batch := helper.CreateBatch(t, infinispan.Name, infinispan.Name, &batchScript, nil, nil)

	helper.WaitForValidBatchPhase(infinispan.Name, v2.BatchRunning)

	job := testKube.GetJob(infinispan.Name, tutils.Namespace)
	podContext := job.Spec.Template.Spec.SecurityContext
	if assert.NotNil(t, podContext) {
		assert.Equal(t, []int64{1000}, podContext.SupplementalGroups) // inherited override
		assert.Equal(t, ptr.To(true), podContext.RunAsNonRoot)        // default preserved
	}
	containerCtx := job.Spec.Template.Spec.Containers[0].SecurityContext
	if assert.NotNil(t, containerCtx) {
		assert.Equal(t, ptr.To(false), containerCtx.ReadOnlyRootFilesystem)   // inherited override
		assert.Equal(t, ptr.To(false), containerCtx.AllowPrivilegeEscalation) // default preserved
		if assert.NotNil(t, containerCtx.Capabilities) {
			assert.Equal(t, []corev1.Capability{"ALL"}, containerCtx.Capabilities.Drop) // default preserved
		}
	}
	testKube.DeleteBatch(batch)
	waitForK8sResourceCleanup(infinispan.Name)
}

func batchString() string {
	batchScript := `create counter --concurrency-level=1 --initial-value=5 --storage=VOLATILE --type=weak batch-counter`
	return strings.ReplaceAll(batchScript, "\t", "")
}

func createCluster(t *testing.T, initializer func(*v1.Infinispan)) *v1.Infinispan {
	infinispan := tutils.DefaultSpec(t, testKube, initializer)
	testKube.Create(infinispan)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, infinispan.Name, tutils.Namespace)
	return infinispan
}

func waitForK8sResourceCleanup(name string) {
	// Ensure that the created Job has completed and has been removed
	err := wait.PollUntilContextTimeout(context.Background(), 10*time.Millisecond, tutils.TestTimeout, false, func(ctx context.Context) (bool, error) {
		return !testKube.AssertK8ResourceExists(name, tutils.Namespace, &batchv1.Job{}), nil
	})
	tutils.ExpectNoError(err)

	// If no Job pods available, then the pods have been garbage collected
	err = wait.PollUntilContextTimeout(context.Background(), tutils.DefaultPollPeriod, tutils.TestTimeout, false, func(ctx context.Context) (bool, error) {
		_, e := batchCtrl.GetJobPodName(name, tutils.Namespace, testKube.Kubernetes.Client, context.Background())
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
