package cache

import (
	"context"
	"fmt"
	"github.com/iancoleman/strcase"
	v1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/api/v2alpha1"
	"github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	testifyAssert "github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"strings"
	"testing"
	"time"
)

var (
	ctx      = context.TODO()
	testKube = tutils.NewTestKubernetes(os.Getenv("TESTING_CONTEXT"))
)

func TestMain(m *testing.M) {
	tutils.RunOperator(m, testKube)
}

func initCluster(t *testing.T, configListener bool) *v1.Infinispan {
	return initClusterWithSuffix(t, "", configListener)
}

func initClusterWithSuffix(t *testing.T, suffix string, configListener bool) *v1.Infinispan {
	spec := tutils.DefaultSpec(t, testKube)
	spec.Name = spec.Name + suffix
	spec.Spec.ConfigListener = &v1.ConfigListenerSpec{
		Enabled: configListener,
	}
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

	ispn := initCluster(t, true)

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
		assertConfigListenerHasNoErrorsOrRestarts(t, ispn)
	}

	//Test for CacheCR with TemplateName
	cache := cacheCR("cache-with-static-template", ispn)
	cache.Spec.TemplateName = "org.infinispan.DIST_SYNC"
	test(cache)

	//Test for CacheCR with TemplateXML
	cache = cacheCR("cache-with-xml-template", ispn)
	cache.Spec.Template = "<distributed-cache name=\"cache-with-xml-template\" mode=\"SYNC\"/>"
	test(cache)
}

func assertConfigListenerHasNoErrorsOrRestarts(t *testing.T, i *v1.Infinispan) {
	// Ensure that the ConfigListener pod is not in a CrashLoopBackOff
	testKube.WaitForDeployment(i.GetConfigListenerName(), tutils.Namespace)
	k8s := testKube.Kubernetes
	podList := testKube.WaitForPods(1, tutils.SinglePodTimeout, &client.ListOptions{
		Namespace: tutils.Namespace,
		LabelSelector: labels.SelectorFromSet(map[string]string{
			"app":         "infinispan-config-listener-pod",
			"clusterName": i.Name,
		})},
		nil,
	)
	testifyAssert.Equal(t, 1, len(podList.Items))

	pod := podList.Items[0]
	testifyAssert.Equal(t, int32(0), pod.Status.ContainerStatuses[0].RestartCount)
	logs, err := k8s.Logs(pod.Name, tutils.Namespace, ctx)
	tutils.ExpectNoError(err)
	testifyAssert.NotContains(t, logs, "ERROR")
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
	if !kubernetes.IsOwnedBy(cr, ispn) {
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
	assertConfigListenerHasNoErrorsOrRestarts(t, ispn)
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

	ispn := tutils.DefaultSpec(t, testKube)
	ispn.Spec.ConfigMapName = configMap.Name
	ispn.Spec.ConfigListener = &v1.ConfigListenerSpec{
		Enabled: true,
	}

	testKube.CreateInfinispan(ispn, tutils.Namespace)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, ispn.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, v1.ConditionWellFormed)

	client := tutils.HTTPClientForCluster(ispn, testKube)
	cacheHelper := tutils.NewCacheHelper(cacheName, client)

	cacheHelper.AssertCacheExists()

	// Assert CR created for static cache and is in the Ready state
	cr := testKube.WaitForCacheConditionReady(cacheName, tutils.Namespace)

	// Assert that the owner reference has been correctly set to the Infinispan CR
	if !kubernetes.IsOwnedBy(cr, ispn) {
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
	assertConfigListenerHasNoErrorsOrRestarts(t, ispn)
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
	assertConfigListenerHasNoErrorsOrRestarts(t, ispn)
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
	assertConfigListenerHasNoErrorsOrRestarts(t, ispn)
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

func TestConfigListenerFailover(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	ispn := initCluster(t, true)
	listOps := &client.ListOptions{
		Namespace:     tutils.Namespace,
		LabelSelector: labels.SelectorFromSet(ispn.PodSelectorLabels()),
	}

	// Scale down, resulting in ConfigLister SSE subscribe to fail and cause a pod restart
	tutils.ExpectNoError(
		testKube.UpdateInfinispan(ispn, func() {
			ispn.Spec.Replicas = 0
		}),
	)
	testKube.WaitForPods(0, tutils.SinglePodTimeout, listOps, nil)

	// Scale up
	tutils.ExpectNoError(
		testKube.UpdateInfinispan(ispn, func() {
			ispn.Spec.Replicas = 1
		}),
	)
	testKube.WaitForPods(1, tutils.SinglePodTimeout, listOps, nil)

	// Create cache via REST
	cacheName := "some-cache"
	cacheConfig := fmt.Sprintf("localCache:\n  memory:\n    maxCount: \"%d\"\n", 100)
	httpClient := tutils.HTTPClientForCluster(ispn, testKube)
	cacheHelper := tutils.NewCacheHelper(cacheName, httpClient)
	cacheHelper.Create(cacheConfig, mime.ApplicationYaml)

	// Assert Cache CR created and ready
	testKube.WaitForCacheConditionReady(cacheName, tutils.Namespace)
	assertConfigListenerHasNoErrorsOrRestarts(t, ispn)
}

func TestCacheResourcesCleanedUpOnDisable(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	ispn := initCluster(t, true)

	// Create cache via REST
	cacheName := "some-cache"
	cacheConfig := "distributedCache: ~"
	httpClient := tutils.HTTPClientForCluster(ispn, testKube)
	cacheHelper := tutils.NewCacheHelper(cacheName, httpClient)
	cacheHelper.Create(cacheConfig, mime.ApplicationYaml)
	cacheHelper.AssertCacheExists()

	// Assert the Cache CR is created
	testKube.WaitForCacheConditionReady(cacheName, tutils.Namespace)

	// Disable the ConfigListener and wait for the Deployment to be removed
	tutils.ExpectNoError(
		testKube.UpdateInfinispan(ispn, func() {
			ispn.Spec.ConfigListener = &v1.ConfigListenerSpec{
				Enabled: false,
			}
		}),
	)
	testKube.WaitForResourceRemoval(ispn.GetConfigListenerName(), tutils.Namespace, &appsv1.Deployment{})

	// Assert the cache still exists on the server
	cacheHelper.AssertCacheExists()

	// Assert that the listener created Cache CR is removed by the Infinispan controller when the ConfigListener is disabled
	testKube.WaitForResourceRemoval(cacheName, tutils.Namespace, &v2alpha1.Cache{})

	// Manually recreate the Cache CR and set its owner reference
	cr := cacheCR(cacheName, ispn)
	cr.ObjectMeta.Annotations = map[string]string{constants.ListenerAnnotationGeneration: "1"}
	cr.Spec.Template = cacheConfig
	tutils.ExpectNoError(controllerutil.SetControllerReference(ispn, cr, testKube.Kubernetes.Client.Scheme()))
	testKube.Create(cr)

	// Delete the cache on the server so the CR becomes stale
	cacheHelper.Delete()

	// Assert the now orphaned Cache CR still exists
	testKube.WaitForCacheConditionReady(cacheName, tutils.Namespace)

	// Enable the ConfigListener again
	tutils.ExpectNoError(
		testKube.UpdateInfinispan(ispn, func() {
			ispn.Spec.ConfigListener = &v1.ConfigListenerSpec{
				Enabled: true,
			}
		}),
	)

	// Assert the Cache CR was removed
	testKube.WaitForResourceRemoval(cacheName, tutils.Namespace, &v2alpha1.Cache{})
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
