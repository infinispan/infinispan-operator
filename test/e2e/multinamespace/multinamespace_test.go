package multinamespace

import (
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/go-logr/zapr"
	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	ctrl "sigs.k8s.io/controller-runtime"
)

var testKube = tutils.NewTestKubernetes(os.Getenv("TESTING_CONTEXT"))

func TestMain(m *testing.M) {
	ctrl.SetLogger(zapr.NewLogger(tutils.Log().Desugar()))

	nsAsString := strings.ToLower(tutils.MultiNamespace)
	namespaces := strings.Split(nsAsString, ",")
	if tutils.RunLocalOperator == "TRUE" {
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
		stopOperator := testKube.RunOperator(nsAsString, "../../../config/crd/bases/")
		code := m.Run()
		stopOperator()
		os.Exit(code)
	} else {
		code := m.Run()
		os.Exit(code)
	}
}

// Test if single node working correctly
func TestMultinamespaceNodeStartup(t *testing.T) {
	// Create a resource without passing any config
	nsAsString := strings.ToLower(tutils.MultiNamespace)
	namespaces := strings.Split(nsAsString, ",")
	var wg sync.WaitGroup
	for _, namespace := range namespaces {
		spec := tutils.DefaultSpec(t, testKube, func(i *ispnv1.Infinispan) {
			i.Namespace = namespace
		})
		// Register it
		testKube.CreateInfinispan(spec, namespace)
		defer testKube.DeleteInfinispan(spec)
		wg.Add(1)
		go func() {
			testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, spec.Namespace)
			testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)
			wg.Done()
		}()
	}
	wg.Wait()
}
