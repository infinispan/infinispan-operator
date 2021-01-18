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
	"github.com/infinispan/infinispan-operator/pkg/controller/infinispan/util"
	tconst "github.com/infinispan/infinispan-operator/test/e2e/constants"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/util"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var testKube = tutils.NewTestKubernetes()

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
		for _, namespace := range namespaces {
			testKube.NewNamespace(namespace)
		}
		testKube.InstallRBAC(namespaces[0], "../../../deploy/")
		testKube.InstallCRD("../../../deploy/crds/")
		stopCh := testKube.RunOperator(nsAsString)
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
		fmt.Printf("Create namespace %s\n", namespace)
		testKube.CreateInfinispan(spec, namespace)
		defer testKube.DeleteInfinispan(spec.Name, spec.Namespace, tconst.SinglePodTimeout)
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
	testKube.WaitForPods("app=infinispan-pod", num, tconst.SinglePodTimeout, spec.Namespace)
	ispn := ispnv1.Infinispan{}
	testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, &ispn)
	protocol := getSchemaForRest(&ispn)
	pods := testKube.GetPods("app=infinispan-pod", spec.Namespace)
	podName := pods[0].Name
	cluster := util.NewCluster(testKube.Kubernetes)

	pass, err := cluster.Kubernetes.GetPassword(cconsts.DefaultOperatorUser, spec.GetSecretName(), spec.Namespace)
	tutils.ExpectNoError(err)

	expectedClusterSize := num
	// Check that the cluster size is num querying the first pod
	var lastErr error
	err = wait.Poll(time.Second, tconst.TestTimeout, func() (done bool, err error) {
		value, err := cluster.GetClusterSize(cconsts.DefaultOperatorUser, pass, podName, spec.Namespace, protocol)
		if err != nil {
			lastErr = err
			return false, nil
		}
		if value > expectedClusterSize {
			return true, fmt.Errorf("more than expected nodes in cluster (expected=%v, actual=%v)", expectedClusterSize, value)
		}

		return value == num, nil
	})

	if err == wait.ErrWaitTimeout && lastErr != nil {
		err = fmt.Errorf("timed out waiting for the condition, last error: %v", lastErr)
	}

	tutils.ExpectNoError(err)
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
	testKube.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: ispn.Namespace, Name: ispn.Name}, &curr)
	if curr.IsEncryptionCertSourceDefined() {
		return "https"
	}
	return "http"
}
