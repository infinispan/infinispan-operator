package multinamespace

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	cconsts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	ispnctrl "github.com/infinispan/infinispan-operator/pkg/controller/infinispan"
	ispn "github.com/infinispan/infinispan-operator/pkg/infinispan"
	tconst "github.com/infinispan/infinispan-operator/test/e2e/constants"
	k8s "github.com/infinispan/infinispan-operator/test/e2e/k8s"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var testKube = k8s.NewTestKubernetes(os.Getenv("TESTING_CONTEXT"))
var serviceAccountKube = k8s.NewTestKubernetes("")

var log = logf.Log.WithName("main_test")

var MinimalSpec = ispnv1.Infinispan{
	TypeMeta: tconst.InfinispanTypeMeta,
	ObjectMeta: metav1.ObjectMeta{
		Name: tconst.DefaultClusterName,
	},
	Spec: ispnv1.InfinispanSpec{
		Replicas: 1,
	},
}

func TestMain(m *testing.M) {
	nsAsString := strings.ToLower(tconst.MultiNamespace)
	namespaces := strings.Split(nsAsString, ",")
	if "TRUE" == tconst.RunLocalOperator {
		for _, namespace := range namespaces {
			testKube.DeleteNamespace(namespace)
		}
		testKube.DeleteCRD("infinispans.infinispan.org")
		testKube.DeleteCRD("caches.infinispan.org")
		testKube.DeleteCRD("backup.infinispan.org")
		testKube.DeleteCRD("restore.infinispan.org")
		for _, namespace := range namespaces {
			testKube.NewNamespace(namespace)
		}
		stopCh := testKube.RunOperator(nsAsString, "../../../deploy/crds/")
		code := m.Run()
		close(stopCh)
		os.Exit(code)
	} else {
		code := m.Run()
		os.Exit(code)
	}
}

// Test if single node working correctly
func TestMultinamespaceNodeStartup(t *testing.T) {
	// Create a resource without passing any config
	nsAsString := strings.ToLower(tconst.MultiNamespace)
	namespaces := strings.Split(nsAsString, ",")
	var wg sync.WaitGroup
	for _, namespace := range namespaces {
		spec := MinimalSpec.DeepCopy()
		spec.Namespace = namespace
		// Register it
		testKube.CreateInfinispan(spec, namespace)
		defer testKube.DeleteInfinispan(spec, tconst.SinglePodTimeout)
		wg.Add(1)
		go func() {
			waitForPodsOrFail(spec, 1)
			wg.Done()
		}()
	}
	wg.Wait()
}

func waitForPodsOrFail(spec *ispnv1.Infinispan, num int) {
	// Wait that "num" pods are up
	testKube.WaitForInfinispanPods(num, tconst.SinglePodTimeout, spec.Name, spec.Namespace)
	ispn := ispnv1.Infinispan{}
	tutils.ExpectNoError(testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, &ispn))
	protocol := getSchemaForRest(&ispn)

	pods := &corev1.PodList{}
	tutils.ExpectNoError(testKube.Kubernetes.ResourcesList(ispn.Namespace, ispnctrl.PodLabels(spec.Name), pods))

	// Check that the cluster size is num querying the first pod
	cluster := tutils.NewCluster(cconsts.DefaultOperatorUser, ispn.GetSecretName(), protocol, ispn.Namespace, testKube.Kubernetes)
	waitForClusterSize(num, pods.Items[0].Name, cluster)
}

func waitForClusterSize(expectedClusterSize int, podName string, cluster *ispn.Cluster) {
	var lastErr error
	err := wait.Poll(time.Second, tconst.TestTimeout, func() (done bool, err error) {
		value, err := AssertClusterSize(expectedClusterSize, podName, cluster)
		if err != nil {
			lastErr = err
			return false, nil
		}
		return value, err
	})

	if err == wait.ErrWaitTimeout && lastErr != nil {
		err = fmt.Errorf("timed out waiting for the condition, last error: %v", lastErr)
	}

	tutils.ExpectNoError(err)
}

func AssertClusterSize(expectedClusterSize int, podName string, cluster *ispn.Cluster) (bool, error) {
	value, err := cluster.GetClusterSize(podName)
	if err != nil {
		return false, err
	}
	if value > expectedClusterSize {
		return true, fmt.Errorf("more than expected nodes in cluster (expected=%v, actual=%v)", expectedClusterSize, value)
	}

	return value == expectedClusterSize, nil
}

func getSchemaForRest(ispn *ispnv1.Infinispan) string {
	curr := ispnv1.Infinispan{}
	// Wait for the operator to populate Infinispan CR data
	err := wait.Poll(tconst.DefaultPollPeriod, tconst.SinglePodTimeout, func() (done bool, err error) {
		testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: ispn.Namespace, Name: ispn.Name}, &curr)
		return len(curr.Status.Conditions) > 0, nil
	})
	if err != nil {
		panic(err.Error())
	}
	return curr.GetEndpointScheme()
}
