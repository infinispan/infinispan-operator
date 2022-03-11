package cache

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/iancoleman/strcase"
	v1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/api/v2alpha1"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

var testKube = tutils.NewTestKubernetes(os.Getenv("TESTING_CONTEXT"))

func TestMain(m *testing.M) {
	tutils.RunOperator(m, testKube)
}

func initCluster(t *testing.T, configListener bool) *v1.Infinispan {
	return initClusterWithSuffix(t, "", configListener)
}

func initClusterWithSuffix(t *testing.T, suffix string, configListener bool) *v1.Infinispan {
	spec := tutils.DefaultSpec(t, testKube, func(i *v1.Infinispan) {
		i.Name = i.Name + suffix
		i.Spec.ConfigListener = &v1.ConfigListenerSpec{
			Enabled: configListener,
		}
	})
	testKube.CreateInfinispan(spec, tutils.Namespace)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, spec.Name, tutils.Namespace)

	ispn := testKube.WaitForInfinispanCondition(spec.Name, spec.Namespace, v1.ConditionWellFormed)

	if configListener {
		testKube.WaitForDeployment(spec.GetConfigListenerName(), tutils.Namespace)
	}
	return ispn
}

func TestCacheCR(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	ispn := initCluster(t, false)

	test := func(cache *v2alpha1.Cache) {
		testKube.Create(cache)
		testKube.WaitForCacheConditionReady(cache.Spec.Name, cache.Namespace)
		client := tutils.HTTPClientForCluster(ispn, testKube)
		cacheHelper := tutils.NewCacheHelper(cache.Spec.Name, client)
		cacheHelper.WaitForCacheToExist()
		cacheHelper.TestBasicUsage("testkey", "test-operator")
		testKube.DeleteCache(cache)
		// Assert that the Cache is removed from the server once the Cache CR has been deleted
		cacheHelper.WaitForCacheToNotExist()
	}

	//Test for CacheCR with Templatename
	cache := cacheCR("cache-with-static-template", ispn)
	cache.Spec.TemplateName = "org.infinispan.DIST_SYNC"
	test(cache)

	//Test for CacheCR with TemplateXML
	cache = cacheCR("cache-with-xml-template", ispn)
	cache.Spec.Template = "<infinispan><cache-container><distributed-cache name=\"cache-with-xml-template\" mode=\"SYNC\"><persistence><file-store/></persistence></distributed-cache></cache-container></infinispan>"
	test(cache)
}

func TestUpdateCacheCR(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	ispn := initCluster(t, false)
	cacheName := ispn.Name
	originalYaml := "localCache:\n  memory:\n    maxCount: 10\n"

	// Create Cache CR with Yaml template
	cr := cacheCR(cacheName, ispn)
	cr.Spec.Template = originalYaml
	testKube.Create(cr)
	cr = testKube.WaitForCacheConditionReady(cacheName, tutils.Namespace)

	validUpdateYaml := strings.Replace(cr.Spec.Template, "10", "50", 1)
	cr.Spec.Template = validUpdateYaml
	testKube.Update(cr)

	// Assert CR spec.Template updated
	testKube.WaitForCacheState(cacheName, tutils.Namespace, func(cache *v2alpha1.Cache) bool {
		return cache.Spec.Template == validUpdateYaml
	})

	// Assert CR remains ready
	cr = testKube.WaitForCacheConditionReady(cacheName, tutils.Namespace)

	invalidUpdateYaml := `distributedCache: {}`
	cr.Spec.Template = invalidUpdateYaml
	testKube.Update(cr)

	// Assert CR spec.Template updated
	testKube.WaitForCacheState(cacheName, tutils.Namespace, func(cache *v2alpha1.Cache) bool {
		return cache.Spec.Template == invalidUpdateYaml
	})

	// Wait for the Cache CR to become unready as the spec.Template cannot be reconciled with the server
	testKube.WaitForCacheCondition(cacheName, tutils.Namespace, v2alpha1.CacheCondition{
		Type:   v2alpha1.CacheConditionReady,
		Status: metav1.ConditionFalse,
	})
}

func TestCacheWithServerLifecycle(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	ispn := initCluster(t, true)
	cacheName := ispn.Name
	yamlTemplate := "localCache:\n  memory:\n    maxCount: \"%d\"\n"
	originalConfig := fmt.Sprintf(yamlTemplate, 100)

	// Create cache via REST
	client := tutils.HTTPClientForCluster(ispn, testKube)
	cacheHelper := tutils.NewCacheHelper(cacheName, client)
	cacheHelper.Create(originalConfig, mime.ApplicationYaml)

	// Assert CR created and ready
	cr := testKube.WaitForCacheConditionReady(cacheName, tutils.Namespace)

	// Assert that the owner reference has been correctly set to the Infinispan CR
	if cr.GetOwnerReferences()[0].UID != ispn.UID {
		panic("Cache has unexpected owner reference")
	}

	// Update cache configuration via REST
	updatedConfig := fmt.Sprintf(yamlTemplate, 50)
	cacheHelper.Update(updatedConfig, mime.ApplicationYaml)

	// Assert CR spec.Template updated
	testKube.WaitForCacheState(cacheName, tutils.Namespace, func(cache *v2alpha1.Cache) bool {
		return cache.Spec.Template == updatedConfig
	})

	// Delete cache via REST
	cacheHelper.Delete()

	// Assert CR deleted
	err := wait.Poll(10*time.Millisecond, tutils.MaxWaitTimeout, func() (bool, error) {
		return !testKube.AssertK8ResourceExists(cacheName, tutils.Namespace, &v2alpha1.Cache{}), nil
	})
	tutils.ExpectNoError(err)
}

func TestStaticServerCache(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	name := strcase.ToKebab(tutils.TestName(t))
	cacheName := "static-cache"

	// Create cluster using custom config containing a static cache
	serverConfig := `
	<infinispan>
		<cache-container name="default">
			<distributed-cache name="static-cache"/>
		</cache-container>
	</infinispan>
	`
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: tutils.Namespace,
		},
		Data: map[string]string{"infinispan-config.xml": serverConfig},
	}
	testKube.CreateConfigMap(configMap)

	ispn := tutils.DefaultSpec(t, testKube, func(i *v1.Infinispan) {
		i.Spec.ConfigMapName = configMap.Name
		i.Spec.ConfigListener = &v1.ConfigListenerSpec{
			Enabled: true,
		}
	})

	testKube.CreateInfinispan(ispn, tutils.Namespace)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, ispn.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, v1.ConditionWellFormed)

	client := tutils.HTTPClientForCluster(ispn, testKube)
	cacheHelper := tutils.NewCacheHelper(cacheName, client)

	cacheHelper.AssertCacheExists()

	// Assert CR created for static cache and is in the Ready state
	cr := testKube.WaitForCacheConditionReady(cacheName, tutils.Namespace)

	// Assert that the owner reference has been correctly set to the Infinispan CR
	if cr.GetOwnerReferences()[0].UID != ispn.UID {
		panic("Cache has unexpected owner reference")
	}

	// Uncomment once ISPN-13492 has been fixed on the server
	// // Assert that deleting the CR does not delete the runtime cache
	// testKube.DeleteCache(cr)
	// // Sleep to wait for potential cache removal on the server
	// time.Sleep(time.Second)
	// if !cacheHelper.Exists() {
	// 	panic("Static cache removed from the server")
	// }
}

func TestCacheWithXML(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	ispn := initCluster(t, true)
	cacheName := ispn.Name
	originalXml := `<local-cache><memory max-count="100"/></local-cache>`

	// Create Cache CR with XML template
	cr := cacheCR(cacheName, ispn)
	cr.Spec.Template = originalXml
	testKube.Create(cr)
	testKube.WaitForCacheConditionReady(cacheName, tutils.Namespace)

	// Wait for 2nd generation of Cache CR with server formatting
	cr = testKube.WaitForCacheState(cacheName, tutils.Namespace, func(cache *v2alpha1.Cache) bool {
		return cache.ObjectMeta.Generation == 2
	})

	// Assert CR spec.Template updated and returned template is in the XML format
	if cr.Spec.Template == originalXml {
		panic("Expected CR template format to be different to original")
	}

	if !strings.Contains(cr.Spec.Template, `memory max-count="100"`) {
		panic("Unexpected cr.Spec.Template content")
	}

	// Update cache via REST
	client := tutils.HTTPClientForCluster(ispn, testKube)
	cacheHelper := tutils.NewCacheHelper(cacheName, client)
	updatedXml := strings.Replace(cr.Spec.Template, "100", "50", 1)
	cacheHelper.Update(updatedXml, mime.ApplicationXml)

	// Assert CR spec.Template updated and returned template is in the XML format
	testKube.WaitForCacheState(cacheName, tutils.Namespace, func(cache *v2alpha1.Cache) bool {
		return cache.Spec.Template == updatedXml
	})
}

func TestCacheWithJSON(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	ispn := initCluster(t, true)
	cacheName := ispn.Name
	originalJson := `{"local-cache":{"memory":{"max-count":"100"}}}`

	// Create Cache CR with XML template
	cr := cacheCR(cacheName, ispn)
	cr.Spec.Template = originalJson
	testKube.Create(cr)
	testKube.WaitForCacheConditionReady(cacheName, tutils.Namespace)

	// Wait for 2nd generation of Cache CR with server formatting
	cr = testKube.WaitForCacheState(cacheName, tutils.Namespace, func(cache *v2alpha1.Cache) bool {
		return cache.ObjectMeta.Generation == 2
	})

	// Assert CR spec.Template updated and returned template is in the JSON format
	if cr.Spec.Template == originalJson {
		panic("Expected CR template format to be different to original")
	}

	if !strings.Contains(cr.Spec.Template, `"100"`) {
		panic("Unexpected cr.Spec.Template content")
	}

	// Update cache via REST
	client := tutils.HTTPClientForCluster(ispn, testKube)
	cacheHelper := tutils.NewCacheHelper(cacheName, client)
	updatedJson := strings.Replace(cr.Spec.Template, "100", "50", 1)
	cacheHelper.Update(updatedJson, mime.ApplicationJson)

	// Assert CR spec.Template updated and returned template is in the JSON format
	testKube.WaitForCacheState(cacheName, tutils.Namespace, func(cache *v2alpha1.Cache) bool {
		return cache.Spec.Template == updatedJson
	})
}

func TestCacheClusterRecreate(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	ispn := initCluster(t, false)

	// Create Cache CR
	cacheName := ispn.Name
	cr := cacheCR(cacheName, ispn)
	cr.Spec.Template = `{"local-cache":{}}`
	testKube.Create(cr)
	testKube.WaitForCacheConditionReady(cacheName, tutils.Namespace)

	// Assert that the cache exists on the server
	client := tutils.HTTPClientForCluster(ispn, testKube)
	cacheHelper := tutils.NewCacheHelper(cacheName, client)
	cacheHelper.WaitForCacheToExist()

	// Delete the original cluster
	testKube.DeleteInfinispan(ispn)

	// Wait for the Cache CR to become unready as the the Infinispan CR no longer exists
	testKube.WaitForCacheCondition(cacheName, tutils.Namespace, v2alpha1.CacheCondition{
		Type:   v2alpha1.CacheConditionReady,
		Status: metav1.ConditionFalse,
	})

	// Recreate the cluster
	ispn = initCluster(t, false)

	// Assert that the cache is recreated and the CR has the ready status
	testKube.WaitForCacheConditionReady(cacheName, tutils.Namespace)

	// Assert that the cache exists on the server
	client = tutils.HTTPClientForCluster(ispn, testKube)
	cacheHelper = tutils.NewCacheHelper(cacheName, client)
	cacheHelper.WaitForCacheToExist()

}

func TestCacheClusterNameChange(t *testing.T) {
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	originalCluster := initClusterWithSuffix(t, "-original-cluster", false)
	newCluster := initClusterWithSuffix(t, "-new-cluster", false)

	// Create Cache CR
	cacheName := "some-cache"
	cr := cacheCR(cacheName, originalCluster)
	cr.Spec.Template = `{"local-cache":{}}`
	testKube.Create(cr)
	testKube.WaitForCacheConditionReady(cacheName, tutils.Namespace)

	// Assert that the cache exists on the original cluster
	client := tutils.HTTPClientForCluster(originalCluster, testKube)
	cacheHelper := tutils.NewCacheHelper(cacheName, client)
	cacheHelper.WaitForCacheToExist()

	// Update Cache CR to point to new cluster
	cr = testKube.GetCache(cr.Name, cr.Namespace)
	cr.ObjectMeta.Labels = newCluster.ObjectMeta.Labels
	cr.Spec.ClusterName = newCluster.Name
	testKube.Update(cr)

	// Assert that the cache exists on the new cluster
	client = tutils.HTTPClientForCluster(newCluster, testKube)
	cacheHelper = tutils.NewCacheHelper(cacheName, client)
	cacheHelper.WaitForCacheToExist()
}

func cacheCR(cacheName string, i *v1.Infinispan) *v2alpha1.Cache {
	return &v2alpha1.Cache{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v2alpha1",
			Kind:       "Cache",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cacheName,
			Namespace: i.Namespace,
			Labels:    i.ObjectMeta.Labels,
		},
		Spec: v2alpha1.CacheSpec{
			ClusterName: i.Name,
			Name:        cacheName,
		},
	}
}
