package multinamespace

import (
	"os"
	"strings"
	"sync"
	"testing"

	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	tconst "github.com/infinispan/infinispan-operator/test/e2e/constants"
	"github.com/infinispan/infinispan-operator/test/e2e/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var testKube = utils.NewTestKubernetes(os.Getenv("TESTING_CONTEXT"))
var serviceAccountKube = utils.NewTestKubernetes("")

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
			testKube.WaitForInfinispanPods(1, tconst.SinglePodTimeout, spec.Name, spec.Namespace)
			wg.Done()
		}()
	}
	wg.Wait()
}
