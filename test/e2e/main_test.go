package e2e

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	"github.com/infinispan/infinispan-operator/pkg/controller/infinispan/util"
	testutil "github.com/infinispan/infinispan-operator/test/e2e/util"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

const TestTimeout = 5 * time.Minute
const SinglePodTimeout = 5 * time.Minute
const RouteTimeout = 240 * time.Second
const DefaultPollPeriod = 1 * time.Second

var CPU = getEnvWithDefault("INFINISPAN_CPU", "500m")
var Memory = getEnvWithDefault("INFINISPAN_MEMORY", "512Mi")
var Namespace = getEnvWithDefault("TESTING_NAMESPACE", "namespace-for-testing")
var RunLocalOperator = getEnvWithDefault("RUN_LOCAL_OPERATOR", "true")

var kubernetes = testutil.NewTestKubernetes()
var cluster = util.NewCluster(kubernetes.Kubernetes)

var DefaultClusterName = "test-node-startup"

var DefaultSpec = ispnv1.Infinispan{
	TypeMeta: testutil.InfinispanTypeMeta,
	ObjectMeta: metav1.ObjectMeta{
		Name: DefaultClusterName,
	},
	Spec: ispnv1.InfinispanSpec{
		Service: ispnv1.InfinispanServiceSpec{
			Type: ispnv1.ServiceTypeDataGrid,
		},
		Container: ispnv1.InfinispanContainerSpec{
			CPU:    CPU,
			Memory: Memory,
		},
		Image:    getEnvWithDefault("IMAGE", "registry.hub.docker.com/infinispan/server"),
		Replicas: 1,
		Expose:   exposeServiceSpec(),
	},
}

var MinimalSpec = ispnv1.Infinispan{
	TypeMeta: metav1.TypeMeta{
		APIVersion: "infinispan.org/v1",
		Kind:       "Infinispan",
	},
	ObjectMeta: metav1.ObjectMeta{
		Name: DefaultClusterName,
	},
	Spec: ispnv1.InfinispanSpec{
		Image:    getEnvWithDefault("IMAGE", "registry.hub.docker.com/infinispan/server"),
		Replicas: 2,
	},
}

func TestMain(m *testing.M) {
	namespace := strings.ToLower(Namespace)
	if "true" == getEnvWithDefault("RUN_LOCAL_OPERATOR", "true") {
		kubernetes.DeleteNamespace(namespace)
		kubernetes.DeleteCRD("infinispans.infinispan.org")
		kubernetes.NewNamespace(namespace)
		stopCh := kubernetes.RunOperator(Namespace)
		code := m.Run()
		close(stopCh)
		os.Exit(code)
	} else {
		code := m.Run()
		os.Exit(code)
	}
}

// Simple smoke test to check if the Kubernetes/OpenShift is alive
func TestSimple(t *testing.T) {
	fmt.Printf("%v\n", kubernetes.Nodes())
	fmt.Printf("%s\n", kubernetes.Kubernetes.PublicIP())
}

// Test if single node working correctly
func TestNodeStartup(t *testing.T) {
	// Create a resource without passing any config
	spec := DefaultSpec.DeepCopy()
	name := "test-node-startup"
	spec.ObjectMeta.Name = name
	// Register it
	kubernetes.CreateInfinispan(spec, Namespace)
	defer kubernetes.DeleteInfinispan(spec, SinglePodTimeout)
	waitForPodsOrFail(spec, 1)
}

// Test if the cluster is working correctly
func TestClusterFormation(t *testing.T) {
	// Create a resource without passing any config
	spec := DefaultSpec.DeepCopy()
	name := "test-cluster-formation"
	spec.ObjectMeta.Name = name
	spec.Spec.Replicas = 2
	// Register it
	kubernetes.CreateInfinispan(spec, Namespace)
	defer kubernetes.DeleteInfinispan(spec, SinglePodTimeout)
	waitForPodsOrFail(spec, 2)
}

// Test if the cluster is working correctly
func TestClusterFormationWithTLS(t *testing.T) {
	// Create a resource without passing any config
	spec := DefaultSpec.DeepCopy()
	name := "test-cluster-formation"
	spec.ObjectMeta.Name = name
	spec.Spec.Replicas = 2
	spec.Spec.Security = ispnv1.InfinispanSecurity{
		EndpointEncryption: ispnv1.EndpointEncryption{
			Type:           "secret",
			CertSecretName: "secret-certs",
		},
	}
	// Create secret
	kubernetes.CreateSecret(&encryptionSecret, Namespace)
	defer kubernetes.DeleteSecret(&encryptionSecret)
	// Register it
	kubernetes.CreateInfinispan(spec, Namespace)
	defer kubernetes.DeleteInfinispan(spec, SinglePodTimeout)
	waitForPodsOrFail(spec, 2)
}

// Test if spec.container.cpu update is handled
func TestContainerCPUUpdateWithTwoReplicas(t *testing.T) {
	var modifier = func(ispn *ispnv1.Infinispan) {
		ispn.Spec.Container.CPU = "250m"
	}
	var verifier = func(ss *appsv1.StatefulSet) {
		limit := resource.MustParse("250m")
		request := resource.MustParse("125m")
		if limit.Cmp(ss.Spec.Template.Spec.Containers[0].Resources.Limits["cpu"]) != 0 ||
			request.Cmp(ss.Spec.Template.Spec.Containers[0].Resources.Requests["cpu"]) != 0 {
			panic("CPU field not updated")
		}
	}
	genericTestForContainerUpdated(MinimalSpec, modifier, verifier)
}

// Test if spec.container.memory update is handled
func TestContainerMemoryUpdate(t *testing.T) {
	var modifier = func(ispn *ispnv1.Infinispan) {
		ispn.Spec.Container.Memory = "256Mi"
	}
	var verifier = func(ss *appsv1.StatefulSet) {
		if resource.MustParse("256Mi") != ss.Spec.Template.Spec.Containers[0].Resources.Requests["memory"] {
			panic("Memory field not updated")
		}
	}
	genericTestForContainerUpdated(DefaultSpec, modifier, verifier)
}

func TestContainerJavaOptsUpdate(t *testing.T) {
	var modifier = func(ispn *ispnv1.Infinispan) {
		ispn.Spec.Container.ExtraJvmOpts = "-XX:NativeMemoryTracking=summary"
	}
	var verifier = func(ss *appsv1.StatefulSet) {
		env := ss.Spec.Template.Spec.Containers[0].Env
		for _, value := range env {
			if value.Name == "JAVA_OPTIONS" {
				if value.Value != "-XX:NativeMemoryTracking=summary" {
					panic("JAVA_OPTIONS not updated")
				} else {
					return
				}
			}
		}
		panic("JAVA_OPTIONS not updated")
	}
	genericTestForContainerUpdated(DefaultSpec, modifier, verifier)
}

// Test if single node working correctly
func genericTestForContainerUpdated(ispn ispnv1.Infinispan, modifier func(*ispnv1.Infinispan), verifier func(*appsv1.StatefulSet)) {
	spec := ispn.DeepCopy()
	name := "test-node-startup"
	spec.ObjectMeta.Name = name
	kubernetes.CreateInfinispan(spec, Namespace)
	defer kubernetes.DeleteInfinispan(spec, SinglePodTimeout)
	waitForPodsOrFail(spec, int(ispn.Spec.Replicas))
	// Get the latest version of the Infinispan resource
	err := kubernetes.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, spec)
	if err != nil {
		panic(err.Error())
	}

	// Get the associate statefulset
	ss := appsv1.StatefulSet{}

	// Get the current generation
	err = kubernetes.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, &ss)
	if err != nil {
		panic(err.Error())
	}
	generation := ss.Status.ObservedGeneration

	// Change the Infinispan spec
	modifier(spec)
	// Workaround for OpenShift local test (clear GVK on decode in the client)
	spec.TypeMeta = testutil.InfinispanTypeMeta
	err = kubernetes.Kubernetes.Client.Update(context.TODO(), spec)
	if err != nil {
		panic(err.Error())
	}

	// Wait for a new generation to appear
	err = wait.Poll(DefaultPollPeriod, SinglePodTimeout, func() (done bool, err error) {
		kubernetes.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, &ss)
		return ss.Status.ObservedGeneration >= generation+1, nil
	})
	if err != nil {
		panic(err.Error())
	}

	// Wait that current and update revisions match
	// this ensure that the rolling upgrade completes
	err = wait.Poll(DefaultPollPeriod, SinglePodTimeout, func() (done bool, err error) {
		kubernetes.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, &ss)
		return ss.Status.CurrentRevision == ss.Status.UpdateRevision, nil
	})
	if err != nil {
		panic(err.Error())
	}

	// Check that the update has been propagated
	verifier(&ss)
}

func TestCacheService(t *testing.T) {
	spec := DefaultSpec.DeepCopy()
	name := "test-cache-service"

	spec.ObjectMeta.Name = name
	spec.Spec.Service.Type = ispnv1.ServiceTypeCache
	spec.Spec.Expose = exposeServiceSpec()

	kubernetes.CreateInfinispan(spec, Namespace)
	defer kubernetes.DeleteInfinispan(spec, SinglePodTimeout)
	waitForPodsOrFail(spec, 1)

	user := "developer"
	password, err := cluster.GetPassword(user, util.GetSecretName(spec), Namespace)
	testutil.ExpectNoError(err)

	routeName := fmt.Sprintf("%s-external", name)
	client := &http.Client{}
	hostAddr := kubernetes.WaitForExternalService(routeName, RouteTimeout, client, user, password, Namespace)

	cacheName := "default"

	key := "test"
	value := "test-operator"
	keyURL := fmt.Sprintf("%v/%v", cacheURL(getSchemaForRest(spec), cacheName, hostAddr), key)
	putViaRoute(keyURL, value, client, user, password)
	actual := getViaRoute(keyURL, client, user, password)

	if actual != value {
		panic(fmt.Errorf("unexpected actual returned: %v (value %v)", actual, value))
	}
}

// TestPermanentCache creates a permanent cache the stop/start
// the cluster and checks that the cache is still there
func TestPermanentCache(t *testing.T) {
	name := "test-permanent-cache"
	usr := "developer"
	cacheName := "test"
	// Define function for the generic stop/start test procedure
	var createPermanentCache = func(ispn *ispnv1.Infinispan) {
		pass, err := cluster.GetPassword(usr, util.GetSecretName(ispn), Namespace)
		testutil.ExpectNoError(err)
		routeName := fmt.Sprintf("%s-external", name)
		client := &http.Client{}
		hostAddr := kubernetes.WaitForExternalService(routeName, RouteTimeout, client, usr, pass, Namespace)
		createCache(getSchemaForRest(ispn), cacheName, usr, pass, hostAddr, "PERMANENT", client)
	}

	var usePermanentCache = func(ispn *ispnv1.Infinispan) {
		pass, err := cluster.GetPassword(usr, util.GetSecretName(ispn), Namespace)
		testutil.ExpectNoError(err)
		routeName := fmt.Sprintf("%s-external", name)
		client := &http.Client{}
		hostAddr := kubernetes.WaitForExternalService(routeName, RouteTimeout, client, usr, pass, Namespace)
		key := "test"
		value := "test-operator"
		keyURL := fmt.Sprintf("%v/%v", cacheURL(getSchemaForRest(ispn), cacheName, hostAddr), key)
		putViaRoute(keyURL, value, client, usr, pass)
		actual := getViaRoute(keyURL, client, usr, pass)
		if actual != value {
			panic(fmt.Errorf("unexpected actual returned: %v (value %v)", actual, value))
		}
		deleteCache(getSchemaForRest(ispn), cacheName, usr, pass, hostAddr, client)
	}

	genericTestForGracefulShutdown(name, createPermanentCache, usePermanentCache)
}

// TestPermanentCache creates a cache with file-store the stop/start
// the cluster and checks that the cache and the data are still there
func TestCheckDataSurviveToShutdown(t *testing.T) {
	name := "test-data-shutdown-cache"
	usr := "developer"
	cacheName := "test"
	template := `<infinispan><cache-container><distributed-cache name ="` + cacheName +
		`"><persistence><file-store/></persistence></distributed-cache></cache-container></infinispan>`
	key := "test"
	value := "test-operator"

	// Define function for the generic stop/start test procedure
	var createCacheWithFileStore = func(ispn *ispnv1.Infinispan) {
		pass, err := cluster.GetPassword(usr, util.GetSecretName(ispn), Namespace)
		testutil.ExpectNoError(err)
		routeName := fmt.Sprintf("%s-external", name)
		client := &http.Client{}
		hostAddr := kubernetes.WaitForExternalService(routeName, RouteTimeout, client, usr, pass, Namespace)
		createCacheWithXMLTemplate(getSchemaForRest(ispn), cacheName, usr, pass, hostAddr, template, client)
		keyURL := fmt.Sprintf("%v/%v", cacheURL(getSchemaForRest(ispn), cacheName, hostAddr), key)
		putViaRoute(keyURL, value, client, usr, pass)
	}

	var useCacheWithFileStore = func(ispn *ispnv1.Infinispan) {
		pass, err := cluster.GetPassword(usr, util.GetSecretName(ispn), Namespace)
		testutil.ExpectNoError(err)
		routeName := fmt.Sprintf("%s-external", name)
		client := &http.Client{}
		hostAddr := kubernetes.WaitForExternalService(routeName, RouteTimeout, client, usr, pass, Namespace)
		schema := getSchemaForRest(ispn)
		keyURL := fmt.Sprintf("%v/%v", cacheURL(schema, cacheName, hostAddr), key)
		actual := getViaRoute(keyURL, client, usr, pass)
		if actual != value {
			panic(fmt.Errorf("unexpected actual returned: %v (value %v)", actual, value))
		}
		deleteCache(schema, cacheName, usr, pass, hostAddr, client)
	}

	genericTestForGracefulShutdown(name, createCacheWithFileStore, useCacheWithFileStore)
}

func genericTestForGracefulShutdown(clusterName string, modifier func(*ispnv1.Infinispan), verifier func(*ispnv1.Infinispan)) {
	// Create a resource without passing any config
	// Register it
	spec := DefaultSpec.DeepCopy()
	spec.ObjectMeta.Name = clusterName
	kubernetes.CreateInfinispan(spec, Namespace)
	defer kubernetes.DeleteInfinispan(spec, SinglePodTimeout)
	waitForPodsOrFail(spec, 1)

	// Do something that needs to be permanent
	modifier(spec)

	// Delete the cluster
	kubernetes.GracefulShutdownInfinispan(spec, SinglePodTimeout)
	kubernetes.GracefulRestartInfinispan(spec, 1, SinglePodTimeout)

	// Do something that checks that permanent changes are there again
	verifier(spec)
}

func waitForPodsOrFail(spec *ispnv1.Infinispan, num int) {
	protocol := getSchemaForRest(spec)
	// Wait that "num" pods are up
	kubernetes.WaitForPods("app=infinispan-pod", num, SinglePodTimeout, Namespace)

	pods := kubernetes.GetPods("app=infinispan-pod", Namespace)
	podName := pods[0].Name

	secretName := util.GetSecretName(spec)
	expectedClusterSize := num
	// Check that the cluster size is num querying the first pod
	var lastErr error
	err := wait.Poll(time.Second, TestTimeout, func() (done bool, err error) {
		value, err := cluster.GetClusterSize(secretName, podName, Namespace, protocol)
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

	testutil.ExpectNoError(err)
}

func getEnvWithDefault(name, defVal string) string {
	str := os.Getenv(name)
	if str != "" {
		return str
	}
	return defVal
}

func TestExternalService(t *testing.T) {
	name := "test-external-service"
	usr := "developer"

	// Create a resource without passing any config
	spec := ispnv1.Infinispan{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v1",
			Kind:       "Infinispan",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: ispnv1.InfinispanSpec{
			Container: ispnv1.InfinispanContainerSpec{
				CPU:    CPU,
				Memory: Memory,
			},
			Image:    getEnvWithDefault("IMAGE", "registry.hub.docker.com/infinispan/server"),
			Replicas: 1,
			Expose:   exposeServiceSpec(),
		},
	}

	// Register it
	kubernetes.CreateInfinispan(&spec, Namespace)
	defer kubernetes.DeleteInfinispan(&spec, SinglePodTimeout)

	kubernetes.WaitForPods("app=infinispan-pod", 1, SinglePodTimeout, Namespace)

	pass, err := cluster.GetPassword(usr, util.GetSecretName(&spec), Namespace)
	testutil.ExpectNoError(err)

	routeName := fmt.Sprintf("%s-external", name)
	client := &http.Client{}
	hostAddr := kubernetes.WaitForExternalService(routeName, RouteTimeout, client, usr, pass, Namespace)

	cacheName := "test"
	schema := getSchemaForRest(&spec)
	createCache(schema, cacheName, usr, pass, hostAddr, "", client)
	defer deleteCache(schema, cacheName, usr, pass, hostAddr, client)

	key := "test"
	value := "test-operator"
	keyURL := fmt.Sprintf("%v/%v", cacheURL(schema, cacheName, hostAddr), key)
	putViaRoute(keyURL, value, client, usr, pass)
	actual := getViaRoute(keyURL, client, usr, pass)

	if actual != value {
		panic(fmt.Errorf("unexpected actual returned: %v (value %v)", actual, value))
	}
}

func exposeServiceSpec() ispnv1.ExposeSpec {
	return ispnv1.ExposeSpec{
		Type:     exposeServiceType(),
		NodePort: 30222,
	}
}

func exposeServiceType() ispnv1.ExposeType {
	exposeServiceType := getEnvWithDefault("EXPOSE_SERVICE_TYPE", "NodePort")
	switch exposeServiceType {
	case "NodePort":
		return ispnv1.ExposeTypeNodePort
	case "LoadBalancer":
		return ispnv1.ExposeTypeLoadBalancer
	default:
		panic(fmt.Errorf("unknown service type %s", exposeServiceType))
	}
}

// TestExternalServiceWithAuth starts a cluster and checks application
// and management connection with authentication
func TestExternalServiceWithAuth(t *testing.T) {
	usr := "connectorusr"
	pass := "connectorpass"
	newpass := "connectornewpass"
	identities := util.CreateIdentitiesFor(usr, pass)
	identitiesYaml, err := yaml.Marshal(identities)
	testutil.ExpectNoError(err)

	// Create secret with application credentials
	secret := corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "conn-secret-test"},
		Type:       "Opaque",
		StringData: map[string]string{"identities.yaml": string(identitiesYaml)},
	}
	kubernetes.CreateSecret(&secret, Namespace)
	defer kubernetes.DeleteSecret(&secret)

	name := "text-external-service-with-auth"

	// Create Infinispan
	spec := ispnv1.Infinispan{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v1",
			Kind:       "Infinispan",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: ispnv1.InfinispanSpec{
			Security: ispnv1.InfinispanSecurity{EndpointSecretName: "conn-secret-test"},
			Container: ispnv1.InfinispanContainerSpec{
				CPU:    CPU,
				Memory: Memory,
			},
			Image:    getEnvWithDefault("IMAGE", "registry.hub.docker.com/infinispan/server"),
			Replicas: 1,
			Expose:   exposeServiceSpec(),
		},
	}
	kubernetes.CreateInfinispan(&spec, Namespace)
	defer kubernetes.DeleteInfinispan(&spec, SinglePodTimeout)

	kubernetes.WaitForPods("app=infinispan-pod", 1, SinglePodTimeout, Namespace)
	schema := getSchemaForRest(&spec)
	testAuthentication(schema, name, usr, pass)
	// Update the auth credentials.
	identities = util.CreateIdentitiesFor(usr, newpass)
	identitiesYaml, err = yaml.Marshal(identities)
	testutil.ExpectNoError(err)

	// Create secret with application credentials
	secret1 := corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "conn-secret-test-1"},
		Type:       "Opaque",
		StringData: map[string]string{"identities.yaml": string(identitiesYaml)},
	}
	kubernetes.CreateSecret(&secret1, Namespace)
	defer kubernetes.DeleteSecret(&secret1)

	// Get the associate statefulset
	ss := appsv1.StatefulSet{}

	// Get the current generation
	err = kubernetes.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, &ss)
	if err != nil {
		panic(err.Error())
	}
	generation := ss.Status.ObservedGeneration

	// Update the resource
	err = kubernetes.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, &spec)
	if err != nil {
		panic(err.Error())
	}

	spec.Spec.Security.EndpointSecretName = "conn-secret-test-1"
	// Workaround for OpenShift local test (clear GVK on decode in the client)
	spec.TypeMeta = testutil.InfinispanTypeMeta
	err = kubernetes.Kubernetes.Client.Update(context.TODO(), &spec)
	if err != nil {
		panic(fmt.Errorf(err.Error()))
	}

	// Wait for a new generation to appear
	err = wait.Poll(DefaultPollPeriod, SinglePodTimeout, func() (done bool, err error) {
		kubernetes.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, &ss)
		return ss.Status.ObservedGeneration >= generation+1, nil
	})
	if err != nil {
		panic(err.Error())
	}
	// Sleep for a while to be sure that the old pods are gone
	// The restart is ongoing and it would that more than 10 sec
	// so we're not introducing any delay
	time.Sleep(10 * time.Second)
	kubernetes.WaitForPods("app=infinispan-pod", 1, SinglePodTimeout, Namespace)
	testAuthentication(schema, name, usr, newpass)
}

func testAuthentication(schema, name, usr, pass string) {
	routeName := fmt.Sprintf("%s-external", name)
	client := &http.Client{}
	hostAddr := kubernetes.WaitForExternalService(routeName, RouteTimeout, client, usr, pass, Namespace)

	cacheName := "test"
	createCacheBadCreds(schema, cacheName, "badUser", "badPass", hostAddr, client)
	createCache(schema, cacheName, usr, pass, hostAddr, "", client)
	defer deleteCache(schema, cacheName, usr, pass, hostAddr, client)

	key := "test"
	value := "test-operator"
	keyURL := fmt.Sprintf("%v/%v", cacheURL(schema, cacheName, hostAddr), key)
	putViaRoute(keyURL, value, client, usr, pass)
	actual := getViaRoute(keyURL, client, usr, pass)

	if actual != value {
		panic(fmt.Errorf("unexpected actual returned: %v (value %v)", actual, value))
	}
}

func cacheURL(schema, cacheName, hostAddr string) string {
	return fmt.Sprintf(schema+"://%v/rest/v2/caches/%s", hostAddr, cacheName)
}

type httpError struct {
	status int
}

func (e *httpError) Error() string {
	return fmt.Sprintf("unexpected response %v", e.status)
}

func createCache(schema, cacheName, usr, pass, hostAddr string, flags string, client *http.Client) {
	httpURL := cacheURL(schema, cacheName, hostAddr)
	fmt.Printf("Create cache: %v\n", httpURL)
	httpEmpty(httpURL, "POST", usr, pass, flags, client)
}

func createCacheBadCreds(schema, cacheName, usr, pass, hostAddr string, client *http.Client) {
	defer func() {
		data := recover()
		if data == nil {
			panic("createCacheBadCred should fail, but it doesn't")
		}
		err := data.(httpError)
		if err.status != http.StatusUnauthorized {
			panic(err)
		}
	}()
	createCache(schema, cacheName, usr, pass, hostAddr, "", client)
}

func createCacheWithXMLTemplate(schema, cacheName, user, pass, hostAddr, template string, client *http.Client) {
	httpURL := cacheURL(schema, cacheName, hostAddr)
	fmt.Printf("Create cache: %v\n", httpURL)
	body := bytes.NewBuffer([]byte(template))
	req, err := http.NewRequest("POST", httpURL, body)
	req.Header.Set("Content-Type", "application/xml;charset=UTF-8")
	req.SetBasicAuth(user, pass)
	fmt.Printf("Put request via route: %v\n", req)
	resp, err := client.Do(req)
	if err != nil {
		panic(err.Error())
	}
	// Accept all the 2xx success codes
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		throwHTTPError(resp)
	}
}

func deleteCache(schema, cacheName, usr, pass, hostAddr string, client *http.Client) {
	httpURL := cacheURL(schema, cacheName, hostAddr)
	fmt.Printf("Delete cache: %v\n", httpURL)
	httpEmpty(httpURL, "DELETE", usr, pass, "", client)
}

func httpEmpty(httpURL string, method string, usr string, pass string, flags string, client *http.Client) {
	req, err := http.NewRequest(method, httpURL, nil)
	if flags != "" {
		req.Header.Set("flags", flags)
	}
	testutil.ExpectNoError(err)

	req.SetBasicAuth(usr, pass)
	resp, err := client.Do(req)
	testutil.ExpectNoError(err)

	if resp.StatusCode != http.StatusOK {
		panic(httpError{resp.StatusCode})
	}
}

func getViaRoute(url string, client *http.Client, user string, pass string) string {
	req, err := http.NewRequest("GET", url, nil)
	req.SetBasicAuth(user, pass)
	resp, err := client.Do(req)
	if err != nil {
		panic(err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		throwHTTPError(resp)
	}
	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		panic(err.Error())
	}
	return string(bodyBytes)
}

func putViaRoute(url string, value string, client *http.Client, user string, pass string) {
	body := bytes.NewBuffer([]byte(value))
	req, err := http.NewRequest("POST", url, body)
	req.Header.Set("Content-Type", "text/plain")
	req.SetBasicAuth(user, pass)
	fmt.Printf("Put request via route: %v\n", req)
	resp, err := client.Do(req)
	if err != nil {
		panic(err.Error())
	}
	if resp.StatusCode != http.StatusNoContent {
		throwHTTPError(resp)
	}
}

func throwHTTPError(resp *http.Response) {
	errorBytes, _ := ioutil.ReadAll(resp.Body)
	panic(fmt.Errorf("unexpected HTTP status code (%d): %s", resp.StatusCode, string(errorBytes)))
}

func getSchemaForRest(ispn *ispnv1.Infinispan) string {
	if ispn.Spec.Security.EndpointEncryption.Type != "" {
		return "https"
	}
	return "http"
}
