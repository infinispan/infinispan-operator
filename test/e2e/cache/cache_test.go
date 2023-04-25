package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/iancoleman/strcase"
	v1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/api/v2alpha1"
	"github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	testifyAssert "github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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
	spec := tutils.DefaultSpec(t, testKube, func(i *v1.Infinispan) {
		i.Name = i.Name + suffix
		i.Spec.ConfigListener = &v1.ConfigListenerSpec{
			Enabled: configListener,
			Logging: &v1.ConfigListenerLoggingSpec{
				Level: v1.ConfigListenerLoggingDebug,
			},
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

	ispn := initCluster(t, true)

	test := func(cache *v2alpha1.Cache) {
		testKube.Create(cache)
		testKube.WaitForCacheConditionReady(cache.Spec.Name, cache.Spec.ClusterName, cache.Namespace)
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

	//Test for CacheCR with spaces in the server cache name
	cache = cacheCR("cache-with-spaces", ispn)
	cache.Spec.Name = "cache with spaces"
	cache.Spec.Template = "<distributed-cache mode=\"SYNC\"/>"
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
	logs, err := k8s.Logs(pod.Name, tutils.Namespace, false, ctx)
	tutils.ExpectNoError(err)
	testifyAssert.NotContains(t, logs, "ERROR", "Error(s) exist in ConfigListener logs")
}

func TestUpdateCacheCR(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	ispn := initCluster(t, true)
	cacheName := ispn.Name
	originalYaml := "localCache:\n  encoding:\n    mediaType: \"text/plain\"\n  memory:\n    maxCount: 10\n  statistics: true\n"

	// Create Cache CR with Yaml template
	cache := cacheCR(cacheName, ispn)
	cache.Spec.Template = originalYaml
	testKube.Create(cache)
	cache = testKube.WaitForCacheConditionReady(cacheName, ispn.Name, tutils.Namespace)
	testifyAssert.Equal(t, int64(1), cache.GetGeneration())

	// Populate cache
	numEntries := 1
	client := tutils.HTTPClientForCluster(ispn, testKube)
	cacheHelper := tutils.NewCacheHelper(cacheName, client)
	cacheHelper.Populate(numEntries)

	validUpdateYaml := strings.Replace(cache.Spec.Template, "10", "50", 1)
	cache.Spec.Template = validUpdateYaml
	testKube.Update(cache)

	type Cache struct {
		Cache struct {
			Memory struct {
				MaxCount string `yaml:"maxCount"`
			} `yaml:"memory"`
		} `yaml:"localCache"`
	}

	// Assert CR spec.Template updated
	testKube.WaitForCacheState(cacheName, ispn.Name, tutils.Namespace, func(cache *v2alpha1.Cache) bool {
		localCache := &Cache{}
		tutils.ExpectNoError(yaml.Unmarshal([]byte(cache.Spec.Template), localCache))
		return localCache.Cache.Memory.MaxCount == "50"
	})
	// Assert CR remains ready
	cache = testKube.WaitForCacheConditionReady(cacheName, ispn.Name, tutils.Namespace)
	testifyAssert.Equal(t, int64(2), cache.GetGeneration())

	// Assert cache content remains
	cacheHelper.AssertSize(numEntries)

	invalidUpdateYaml := strings.Replace(cache.Spec.Template, "text/plain", "application/x-java-serialized-object", 1)
	cache.Spec.Template = invalidUpdateYaml
	testKube.Update(cache)

	// Assert CR spec.Template updated
	cache = testKube.WaitForCacheState(cacheName, ispn.Name, tutils.Namespace, func(cache *v2alpha1.Cache) bool {
		return cache.Spec.Template == invalidUpdateYaml
	})
	testifyAssert.Equal(t, int64(3), cache.GetGeneration())

	// Wait for the Cache CR to become unready as the spec.Template cannot be reconciled with the server
	testKube.WaitForCacheCondition(cacheName, ispn.Name, tutils.Namespace, v2alpha1.CacheCondition{
		Type:   v2alpha1.CacheConditionReady,
		Status: metav1.ConditionFalse,
	})

	// Assert original cache
	cacheHelper.AssertSize(numEntries)
}

func TestUpdateRecreateStrategy(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	ispn := initCluster(t, true)
	cacheName := ispn.Name
	originalYaml := "localCache:\n  encoding:\n    mediaType: \"text/plain\"\n  memory:\n    maxCount: 10\n  statistics: true\n"

	// Create Cache CR with Yaml template
	cache := cacheCR(cacheName, ispn)
	cache.Spec.Updates.Strategy = v2alpha1.CacheUpdateRecreate
	cache.Spec.Template = originalYaml
	testKube.Create(cache)
	cache = testKube.WaitForCacheConditionReady(cacheName, ispn.Name, tutils.Namespace)
	testifyAssert.Equal(t, int64(1), cache.GetGeneration())

	// Populate cache
	numEntries := 1
	client := tutils.HTTPClientForCluster(ispn, testKube)
	cacheHelper := tutils.NewCacheHelper(cacheName, client)
	cacheHelper.Populate(numEntries)

	validUpdateYaml := strings.Replace(cache.Spec.Template, "10", "50", 1)
	cache.Spec.Template = validUpdateYaml
	testKube.Update(cache)

	type Cache struct {
		Cache struct {
			Memory struct {
				MaxCount string `yaml:"maxCount"`
			} `yaml:"memory"`
		} `yaml:"localCache"`
	}

	// Assert CR spec.Template updated
	testKube.WaitForCacheState(cacheName, ispn.Name, tutils.Namespace, func(cache *v2alpha1.Cache) bool {
		localCache := &Cache{}
		tutils.ExpectNoError(yaml.Unmarshal([]byte(cache.Spec.Template), localCache))
		return localCache.Cache.Memory.MaxCount == "50"
	})
	// Assert CR remains ready
	cache = testKube.WaitForCacheConditionReady(cacheName, ispn.Name, tutils.Namespace)
	testifyAssert.Equal(t, int64(2), cache.GetGeneration())

	// Assert cache content remains
	cacheHelper.AssertSize(numEntries)

	recreateUpdateYaml := strings.ReplaceAll(cache.Spec.Template, "text/plain", "application/x-java-serialized-object")
	cache.Spec.Template = recreateUpdateYaml
	testKube.Update(cache)

	// Assert CR spec.Template updated
	cache = testKube.WaitForCacheState(cacheName, ispn.Name, tutils.Namespace, func(cache *v2alpha1.Cache) bool {
		return cache.Spec.Template == recreateUpdateYaml
	})
	testifyAssert.Equal(t, int64(3), cache.GetGeneration())
	testKube.WaitForCacheConditionReady(cacheName, ispn.Name, tutils.Namespace)

	// Assert cache recreated on the server
	cacheHelper.AssertSize(0)
}

func TestCacheWithServerLifecycle(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	ispn := initCluster(t, true)
	// Create the cache on the server with / to ensure listener converts to a k8s friendly name
	cacheName := strings.Replace(ispn.Name, "-", "/", -1)
	yamlTemplate := "localCache: \n  memory: \n    maxCount: \"%d\"\n"
	originalConfig := fmt.Sprintf(yamlTemplate, 100)

	// Create cache via REST
	client := tutils.HTTPClientForCluster(ispn, testKube)
	cacheHelper := tutils.NewCacheHelper(cacheName, client)
	cacheHelper.Create(originalConfig, mime.ApplicationYaml)

	// Assert CR created and ready
	cr := testKube.WaitForCacheConditionReady(cacheName, ispn.Name, tutils.Namespace)

	// Ensure that the listener has created the CR name with the correct format
	testifyAssert.True(t, strings.HasPrefix(cr.Name, strings.Replace(cacheName, "/", "-", -1)))

	// Assert that the owner reference has been correctly set to the Infinispan CR
	testifyAssert.True(t, kubernetes.IsOwnedBy(cr, ispn), "Cache has unexpected owner reference")

	// Update cache configuration via REST

	updatedConfig := fmt.Sprintf(yamlTemplate, 50)
	// Assert CR spec.Template updated, two retries max
	err := wait.PollImmediate(10*time.Second, 11*time.Second, func() (bool, error) {
		cacheHelper.Update(updatedConfig, mime.ApplicationYaml)
		cache := testKube.FindCacheResource(cacheName, ispn.Name, tutils.Namespace)
		return cache != nil && cache.Spec.Template == updatedConfig, nil
	})
	tutils.ExpectNoError(err)

	// Delete cache via REST
	cacheHelper.Delete()

	// Assert CR deleted
	err = wait.Poll(10*time.Millisecond, tutils.MaxWaitTimeout, func() (bool, error) {
		return !testKube.AssertK8ResourceExists(cr.Name, tutils.Namespace, &v2alpha1.Cache{}), nil
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
	defer testKube.DeleteConfigMap(configMap)

	ispn := tutils.DefaultSpec(t, testKube, func(i *v1.Infinispan) {
		i.Spec.ConfigMapName = configMap.Name
		i.Spec.ConfigListener = &v1.ConfigListenerSpec{
			Enabled: true,
			Logging: &v1.ConfigListenerLoggingSpec{
				Level: v1.ConfigListenerLoggingDebug,
			},
		}
	})

	testKube.CreateInfinispan(ispn, tutils.Namespace)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, ispn.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, v1.ConditionWellFormed)

	client := tutils.HTTPClientForCluster(ispn, testKube)
	cacheHelper := tutils.NewCacheHelper(cacheName, client)

	cacheHelper.AssertCacheExists()

	// Assert CR created for static cache and is in the Ready state
	cr := testKube.WaitForCacheConditionReady(cacheName, ispn.Name, tutils.Namespace)

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
	cache := cacheCR(cacheName, ispn)
	cache.Spec.Template = originalXml
	testKube.Create(cache)
	cache = testKube.WaitForCacheConditionReady(cacheName, ispn.Name, tutils.Namespace)
	testifyAssert.Equal(t, int64(1), cache.GetGeneration())

	if !strings.Contains(cache.Spec.Template, `memory max-count="100"`) {
		panic("Unexpected cache.Spec.Template content")
	}

	// Update cache via REST
	client := tutils.HTTPClientForCluster(ispn, testKube)
	cacheHelper := tutils.NewCacheHelper(cacheName, client)
	updatedXml := strings.Replace(cache.Spec.Template, "100", "50", 1)
	cacheHelper.Update(updatedXml, mime.ApplicationXml)

	// Assert CR spec.Template updated
	testKube.WaitForCacheState(cacheName, ispn.Name, tutils.Namespace, func(cache *v2alpha1.Cache) bool {
		return strings.Contains(cache.Spec.Template, "50")
	})
	assertConfigListenerHasNoErrorsOrRestarts(t, ispn)
}

func TestCacheWithJSON(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	ispn := initCluster(t, true)
	cacheName := ispn.Name
	originalJson := `{"local-cache":{"memory":{"max-count":"100"},"statistics":true}}`

	// Create Cache CR with JSON template
	cache := cacheCR(cacheName, ispn)
	cache.Spec.Template = originalJson
	testKube.Create(cache)
	cache = testKube.WaitForCacheConditionReady(cacheName, ispn.Name, tutils.Namespace)

	// Assert CR spec.Template is the same as the original JSON as no transformations are required
	testifyAssert.Equal(t, originalJson, cache.Spec.Template)
	testifyAssert.Equal(t, int64(1), cache.GetGeneration())

	// Update cache via REST
	client := tutils.HTTPClientForCluster(ispn, testKube)
	cacheHelper := tutils.NewCacheHelper(cacheName, client)
	updatedJson := strings.Replace(cache.Spec.Template, "100", "50", 1)
	cacheHelper.Update(updatedJson, mime.ApplicationJson)

	type Cache struct {
		Cache struct {
			Memory struct {
				MaxCount string `json:"max-count"`
			} `json:"memory"`
		} `json:"local-cache"`
	}

	// Assert CR spec.Template updated and returned template is in the JSON format
	cache = testKube.WaitForCacheState(cacheName, ispn.Name, tutils.Namespace, func(cache *v2alpha1.Cache) bool {
		localCache := &Cache{}
		tutils.ExpectNoError(json.Unmarshal([]byte(cache.Spec.Template), localCache))
		return localCache.Cache.Memory.MaxCount == "50"
	})
	testifyAssert.Equal(t, int64(2), cache.GetGeneration())
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
	testKube.WaitForCacheConditionReady(cacheName, ispn.Name, tutils.Namespace)

	// Assert that the cache exists on the server
	client := tutils.HTTPClientForCluster(ispn, testKube)
	cacheHelper := tutils.NewCacheHelper(cacheName, client)
	cacheHelper.WaitForCacheToExist()

	// Delete the original cluster
	testKube.DeleteInfinispan(ispn)

	// Wait for the Cache CR to become unready as the the Infinispan CR no longer exists
	testKube.WaitForCacheCondition(cacheName, ispn.Name, tutils.Namespace, v2alpha1.CacheCondition{
		Type:   v2alpha1.CacheConditionReady,
		Status: metav1.ConditionFalse,
	})

	// Recreate the cluster
	ispn = initCluster(t, false)

	// Assert that the cache is recreated and the CR has the ready status
	testKube.WaitForCacheConditionReady(cacheName, ispn.Name, tutils.Namespace)

	// Assert that the cache exists on the server
	client = tutils.HTTPClientForCluster(ispn, testKube)
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
	testKube.WaitForCacheConditionReady(cacheName, ispn.Name, tutils.Namespace)
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
	cache := testKube.WaitForCacheConditionReady(cacheName, ispn.Name, tutils.Namespace)

	// Disable the ConfigListener and wait for the Deployment to be removed
	tutils.ExpectNoError(
		testKube.UpdateInfinispan(ispn, func() {
			ispn.Spec.ConfigListener.Enabled = false
		}),
	)
	testKube.WaitForResourceRemoval(ispn.GetConfigListenerName(), tutils.Namespace, &appsv1.Deployment{})

	// Assert the cache still exists on the server
	cacheHelper.AssertCacheExists()

	// Assert that the listener created Cache CR is removed by the Infinispan controller when the ConfigListener is disabled
	testKube.WaitForResourceRemoval(cache.Name, tutils.Namespace, &v2alpha1.Cache{})

	// Manually recreate the Cache CR and set its owner reference
	cr := cacheCR(cacheName, ispn)
	cr.ObjectMeta.Annotations = map[string]string{constants.ListenerAnnotationGeneration: "1"}
	cr.Spec.Template = cacheConfig
	tutils.ExpectNoError(controllerutil.SetControllerReference(ispn, cr, testKube.Kubernetes.Client.Scheme()))
	testKube.Create(cr)

	// Delete the cache on the server so the CR becomes stale
	cacheHelper.Delete()

	// Assert the now orphaned Cache CR still exists
	testKube.WaitForCacheConditionReady(cacheName, ispn.Name, tutils.Namespace)

	// Enable the ConfigListener again
	tutils.ExpectNoError(
		testKube.UpdateInfinispan(ispn, func() {
			ispn.Spec.ConfigListener.Enabled = true
		}),
	)

	// Assert the Cache CR was removed
	testKube.WaitForResourceRemoval(cacheName, tutils.Namespace, &v2alpha1.Cache{})
}

func TestSameCacheNameInMultipleClusters(t *testing.T) {
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	// Create two clusters in the same namespace
	cluster1 := initClusterWithSuffix(t, "-c1", true)
	cluster2 := initClusterWithSuffix(t, "-c2", true)

	// Create a cache with the same name on each cluster
	cacheName := "name-collision"
	cacheConfig := "distributedCache: ~"
	cluster1Cache := tutils.NewCacheHelper(cacheName, tutils.HTTPClientForCluster(cluster1, testKube))
	cluster1Cache.Create(cacheConfig, mime.ApplicationYaml)
	cluster1Cache.WaitForCacheToExist()

	cluster2Cache := tutils.NewCacheHelper(cacheName, tutils.HTTPClientForCluster(cluster2, testKube))
	cluster2Cache.Create(cacheConfig, mime.ApplicationYaml)
	cluster2Cache.WaitForCacheToExist()

	// Wait for a Cache CR to be created for each cluster
	cluster1CR := testKube.WaitForCacheConditionReady(cacheName, cluster1.Name, tutils.Namespace)
	cluster2CR := testKube.WaitForCacheConditionReady(cacheName, cluster2.Name, tutils.Namespace)

	// Assert that the two CRs are distinct and have the expected cluster defined in the spec
	testifyAssert.NotEqualf(t, cluster1CR.UID, cluster2CR.UID, "Cache CR UUID should be different")
	testifyAssert.Equal(t, cluster1.Name, cluster1CR.Spec.ClusterName)
	testifyAssert.Equal(t, cluster2.Name, cluster2CR.Spec.ClusterName)
}

func TestCacheCRCacheService(t *testing.T) {
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	ispn := tutils.DefaultSpec(t, testKube, func(i *v1.Infinispan) {
		i.Spec.Service.Type = v1.ServiceTypeCache
	})
	testKube.CreateInfinispan(ispn, tutils.Namespace)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, ispn.Name, tutils.Namespace)

	cache := cacheCR("some-cache", ispn)
	testKube.Create(cache)
	testKube.WaitForCacheConditionReady(cache.Spec.Name, cache.Spec.ClusterName, cache.Namespace)
	client := tutils.HTTPClientForCluster(ispn, testKube)
	cacheHelper := tutils.NewCacheHelper(cache.Spec.Name, client)
	cacheHelper.WaitForCacheToExist()
	cacheHelper.TestBasicUsage("testkey", "test-operator")
	testKube.DeleteCache(cache)
	// Assert that the Cache is removed from the server once the Cache CR has been deleted
	cacheHelper.WaitForCacheToNotExist()
}

func cacheCR(cacheName string, i *v1.Infinispan) *v2alpha1.Cache {
	cache := &v2alpha1.Cache{
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
	cache.Default()
	return cache
}
