package cache

import (
	"os"
	"testing"

	"github.com/iancoleman/strcase"
	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/api/v2alpha1"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var testKube = tutils.NewTestKubernetes(os.Getenv("TESTING_CONTEXT"))

func TestMain(m *testing.M) {
	tutils.RunOperator(m, testKube)
}

func TestCacheCR(t *testing.T) {
	t.Parallel()
	spec := tutils.DefaultSpec(testKube)
	name := strcase.ToKebab(t.Name())
	spec.Name = name
	spec.Labels = map[string]string{"test-name": t.Name()}
	testKube.CreateInfinispan(spec, tutils.Namespace)
	defer testKube.CleanNamespaceAndLogOnPanic(tutils.Namespace, spec.Labels)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)
	ispn := testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, ispnv1.ConditionWellFormed)

	//Test for CacheCR with Templatename

	cacheCRTemplateName := createCacheWithCR("cache-with-static-template", spec.Namespace, name)
	cacheCRTemplateName.Spec.TemplateName = "org.infinispan.DIST_SYNC"
	testCacheWithCR(ispn, cacheCRTemplateName)

	//Test for CacheCR with TemplateXML

	cacheCRTemplateXML := createCacheWithCR("cache-with-xml-template", spec.Namespace, name)
	cacheCRTemplateXML.Spec.Template = "<infinispan><cache-container><distributed-cache name=\"cache-with-xml-template\" mode=\"SYNC\"><persistence><file-store/></persistence></distributed-cache></cache-container></infinispan>"
	testCacheWithCR(ispn, cacheCRTemplateXML)
}

func testCacheWithCR(ispn *ispnv1.Infinispan, cache *v2alpha1.Cache) {
	key := "testkey"
	value := "test-operator"
	testKube.Create(cache)
	hostAddr, client := tutils.HTTPClientAndHost(ispn, testKube)
	condition := v2alpha1.CacheCondition{
		Type:   "Ready",
		Status: "True",
	}
	testKube.WaitForCacheCondition(cache.Spec.Name, cache.Namespace, condition)
	tutils.WaitForCacheToBeCreated(cache.Spec.Name, hostAddr, client)
	tutils.TestBasicCacheUsage(key, value, cache.Spec.Name, hostAddr, client)
	defer testKube.DeleteCache(cache)
}
func createCacheWithCR(cacheName string, nameSpace string, clusterName string) *v2alpha1.Cache {
	return &v2alpha1.Cache{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v2alpha1",
			Kind:       "Cache",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cacheName,
			Namespace: nameSpace,
		},
		Spec: v2alpha1.CacheSpec{
			ClusterName: clusterName,
			Name:        cacheName,
		},
	}
}
