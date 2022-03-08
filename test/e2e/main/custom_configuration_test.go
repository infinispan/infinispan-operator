package main

import (
	"fmt"
	"testing"

	"github.com/iancoleman/strcase"
	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	tutils "github.com/infinispan/infinispan-operator/test/e2e/utils"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

// Test custom configuration with cache-container element
func TestUserXmlCustomConfig(t *testing.T) {
	t.Parallel()
	configMap := newCustomConfigMap(t.Name(), "xml")
	testCustomConfig(t, configMap)
}

func TestUserYamlCustomConfig(t *testing.T) {
	t.Parallel()
	configMap := newCustomConfigMap(t.Name(), "yaml")
	testCustomConfig(t, configMap)
}

func TestUserJsonCustomConfig(t *testing.T) {
	t.Parallel()
	configMap := newCustomConfigMap(t.Name(), "json")
	testCustomConfig(t, configMap)
}

func testCustomConfig(t *testing.T, configMap *corev1.ConfigMap) {
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	addLog4jToCustomConfigMap(configMap)
	testKube.Create(configMap)
	defer testKube.DeleteConfigMap(configMap)

	// Create a resource without passing any config
	ispn := tutils.DefaultSpec(t, testKube)
	ispn.Spec.ConfigMapName = configMap.Name

	// Register it
	testKube.CreateInfinispan(ispn, tutils.Namespace)
	testKube.WaitForInfinispanPods(1, tutils.SinglePodTimeout, ispn.Name, tutils.Namespace)
	testKube.WaitForInfinispanCondition(ispn.Name, ispn.Namespace, ispnv1.ConditionWellFormed)

	client_ := tutils.HTTPClientForCluster(ispn, testKube)
	cacheHelper := tutils.NewCacheHelper(t.Name(), client_)
	cacheHelper.TestBasicUsage("testkey", "test-operator")

	sts := testKube.GetStatefulSet(ispn.Name, ispn.Namespace)
	if sts.Spec.Template.Spec.Containers[0].Args[2] != "user/log4j.xml" {
		tutils.ExpectNoError(fmt.Errorf("failed to pass the custom log4j.xml logging config as an argument "+
			"of the Infinispan server (%s)", sts.Name))
	}
}

// TestUserCustomConfigWithAuthUpdate tests that user custom config works well with update
// using authentication update to trigger a cluster update
func TestUserCustomConfigWithAuthUpdate(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	configMap := newCustomConfigMap(t.Name(), "xml")
	testKube.Create(configMap)
	defer testKube.DeleteConfigMap(configMap)

	var modifier = func(ispn *ispnv1.Infinispan) {
		// testing cache pre update
		client_ := tutils.HTTPClientForCluster(ispn, testKube)
		cacheHelper := tutils.NewCacheHelper(t.Name(), client_)
		cacheHelper.TestBasicUsage("testkey", "test-operator")
		ispn.Spec.Security.EndpointAuthentication = pointer.BoolPtr(true)
	}
	var verifier = func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		testKube.WaitForInfinispanCondition(ss.Name, ss.Namespace, ispnv1.ConditionWellFormed)
		// testing cache post update
		client_ := tutils.HTTPClientForCluster(ispn, testKube)
		cacheHelper := tutils.NewCacheHelper(t.Name(), client_)
		cacheHelper.TestBasicUsage("testkey", "test-operator")
	}
	ispn := tutils.DefaultSpec(t, testKube)
	ispn.Spec.Security.EndpointAuthentication = pointer.BoolPtr(false)
	ispn.Spec.ConfigMapName = configMap.Name
	genericTestForContainerUpdated(*ispn, modifier, verifier)
}

// TestUserCustomConfigUpdateOnNameChange tests that user custom config works well with user config update
func TestUserCustomConfigUpdateOnNameChange(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	configMap := newCustomConfigMap(t.Name(), "xml")
	testKube.Create(configMap)
	defer testKube.DeleteConfigMap(configMap)
	configMapChanged := newCustomConfigMap(t.Name()+"Changed", "xml")
	testKube.Create(configMapChanged)
	defer testKube.DeleteConfigMap(configMapChanged)

	var modifier = func(ispn *ispnv1.Infinispan) {
		// testing cache pre update
		client_ := tutils.HTTPClientForCluster(ispn, testKube)
		cacheHelper := tutils.NewCacheHelper(t.Name(), client_)
		cacheHelper.TestBasicUsage("testkey", "test-operator")
		ispn.Spec.ConfigMapName = configMapChanged.Name
	}
	var verifier = func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		testKube.WaitForInfinispanCondition(ss.Name, ss.Namespace, ispnv1.ConditionWellFormed)
		// testing cache post update
		client_ := tutils.HTTPClientForCluster(ispn, testKube)
		cacheHelper := tutils.NewCacheHelper(t.Name()+"Changed", client_)
		cacheHelper.TestBasicUsage("testkey", "test-operator")
	}
	ispn := tutils.DefaultSpec(t, testKube)
	ispn.Spec.Security.EndpointAuthentication = pointer.BoolPtr(false)
	ispn.Spec.ConfigMapName = configMap.Name
	genericTestForContainerUpdated(*ispn, modifier, verifier)
}

func TestUserCustomConfigUpdateOnChange(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	configMap := newCustomConfigMap(t.Name(), "xml")
	testKube.Create(configMap)
	defer testKube.DeleteConfigMap(configMap)

	newCacheName := t.Name() + "Updated"
	var modifier = func(ispn *ispnv1.Infinispan) {
		// testing cache pre update
		client_ := tutils.HTTPClientForCluster(ispn, testKube)
		cacheHelper := tutils.NewCacheHelper(t.Name(), client_)
		cacheHelper.TestBasicUsage("testkey", "test-operator")
		configMapUpdated := newCustomConfigMap(newCacheName, "xml")
		// Reuse old name to test CM in-place update
		configMapUpdated.Name = strcase.ToKebab(t.Name())
		testKube.UpdateConfigMap(configMapUpdated)
	}
	var verifier = func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		testKube.WaitForInfinispanCondition(ss.Name, ss.Namespace, ispnv1.ConditionWellFormed)
		// testing cache post update
		client_ := tutils.HTTPClientForCluster(ispn, testKube)
		cacheHelper := tutils.NewCacheHelper(newCacheName, client_)
		cacheHelper.TestBasicUsage("testkey", "test-operator")
	}
	ispn := tutils.DefaultSpec(t, testKube)
	ispn.Spec.Security.EndpointAuthentication = pointer.BoolPtr(false)
	ispn.Spec.ConfigMapName = configMap.Name
	genericTestForContainerUpdated(*ispn, modifier, verifier)
}

// TestUserCustomConfigUpdateOnAdd tests that user custom config works well with user config update
func TestUserCustomConfigUpdateOnAdd(t *testing.T) {
	t.Parallel()
	defer testKube.CleanNamespaceAndLogOnPanic(t, tutils.Namespace)

	configMap := newCustomConfigMap(t.Name(), "xml")
	testKube.Create(configMap)
	defer testKube.DeleteConfigMap(configMap)

	var modifier = func(ispn *ispnv1.Infinispan) {
		tutils.ExpectNoError(testKube.UpdateInfinispan(ispn, func() {
			ispn.Spec.ConfigMapName = configMap.Name
		}))
	}
	var verifier = func(ispn *ispnv1.Infinispan, ss *appsv1.StatefulSet) {
		testKube.WaitForInfinispanCondition(ss.Name, ss.Namespace, ispnv1.ConditionWellFormed)
		// testing cache post update
		client_ := tutils.HTTPClientForCluster(ispn, testKube)
		cacheHelper := tutils.NewCacheHelper(t.Name(), client_)
		cacheHelper.TestBasicUsage("testkey", "test-operator")
	}
	ispn := tutils.DefaultSpec(t, testKube)
	ispn.Spec.Security.EndpointAuthentication = pointer.BoolPtr(false)
	genericTestForContainerUpdated(*ispn, modifier, verifier)
}

func newCustomConfigMap(name, format string) *corev1.ConfigMap {
	var userCacheContainer string
	switch format {
	case "xml":
		userCacheContainer = `<infinispan
xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
xsi:schemaLocation="urn:infinispan:config:13.0 http://www.infinispan.org/schemas/infinispan-config-13.0.xsd
					urn:infinispan:server:13.0 http://www.infinispan.org/schemas/infinispan-server-13.0.xsd"
xmlns="urn:infinispan:config:13.0"
xmlns:server="urn:infinispan:server:13.0">
	<cache-container name="default" statistics="true">
		<distributed-cache name="` + name + `"/>
	</cache-container>
</infinispan>`
	case "yaml":
		userCacheContainer = `infinispan:
  cacheContainer:
  	name: default
		distributedCache:
			name: ` + name
	case "json":
		userCacheContainer = `{ "infinispan": { "cacheContainer": { "name": "default", "distributedCache": { "name": "` + name + `"}}}}`
	}

	return &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: strcase.ToKebab(name),
		Namespace: tutils.Namespace},
		Data: map[string]string{"infinispan-config." + format: userCacheContainer},
	}
}

func addLog4jToCustomConfigMap(customConfigMap *corev1.ConfigMap) {
	// Add overlay Log4j config to custom ConfigMap
	customConfigMap.Data["log4j.xml"] = `<?xml version="1.0" encoding="UTF-8"?>
<Configuration name="InfinispanServerConfig" monitorInterval="60" shutdownHook="disable">
    <Appenders>
        <!-- Colored output on the console -->
        <Console name="STDOUT">
            <PatternLayout pattern="%d{HH:mm:ss,SSS} %-5p (%t) [%c] %m%throwable%n"/>
        </Console>
    </Appenders>

    <Loggers>
        <Root level="INFO">
            <AppenderRef ref="STDOUT" level="TRACE"/>
        </Root>
        <Logger name="org.infinispan" level="TRACE"/>
    </Loggers>
</Configuration>`
}
