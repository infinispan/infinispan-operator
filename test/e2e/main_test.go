package e2e

import (
	"bytes"
	"context"
	"fmt"
	"github.com/infinispan/infinispan-operator/pkg/controller/infinispan/util"
	testutil "github.com/infinispan/infinispan-operator/test/e2e/util"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
)

const Namespace = "namespace-for-testing"
const TestTimeout = 5 * time.Minute
const SinglePodTimeout = 5 * time.Minute
const RouteTimeout = 240 * time.Second

var CPU = getEnvWithDefault("INFINISPAN_CPU", "0.5")
var Memory = getEnvWithDefault("INFINISPAN_MEMORY", "512Mi")

var kubernetes = testutil.NewTestKubernetes()
var cluster = util.NewCluster(kubernetes.Kubernetes)

func TestMain(m *testing.M) {
	namespace := strings.ToLower(Namespace)
	kubernetes.DeleteNamespace(namespace)
	kubernetes.DeleteCRD("infinispans.infinispan.org")
	kubernetes.NewNamespace(namespace)
	stopCh := kubernetes.RunOperator(Namespace)
	code := m.Run()
	close(stopCh)
	os.Exit(code)
}

// Simple smoke test to check if the Kubernetes/OpenShift is alive
func TestSimple(t *testing.T) {
	fmt.Printf("%v\n", kubernetes.Nodes())
	fmt.Printf("%s\n", kubernetes.PublicIP())
}

// Test if the cluster is working correctly
func TestClusterFormation(t *testing.T) {
	// Create a resource without passing any config
	name := "test-cluster-formation"
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
			Replicas: 2,
		},
	}
	// Register it
	kubernetes.CreateInfinispan(&spec, Namespace)
	defer kubernetes.DeleteInfinispan(&spec, SinglePodTimeout)
	waitForPodsOrFail(name, "http")
}

// Test if the cluster is working correctly
func TestClusterFormationWithTLS(t *testing.T) {
	// Create a resource without passing any config
	name := "test-cluster-formation"
	kubernetes.CreateSecret(&encryptionSecret, Namespace)
	defer kubernetes.DeleteSecret(&encryptionSecret)
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
			Replicas: 2,
			Security: ispnv1.InfinispanSecurity{
				EndpointEncryption: ispnv1.EndpointEncryption{
					Type:           "secret",
					CertSecretName: "secret-certs",
				},
			},
		},
	}
	// Register it
	kubernetes.CreateInfinispan(&spec, Namespace)
	defer kubernetes.DeleteInfinispan(&spec, SinglePodTimeout)
	waitForPodsOrFail(name, "https")
}

func waitForPodsOrFail(name, protocol string) {
	// Wait that 2 pods are up
	kubernetes.WaitForPods("app=infinispan-pod", 2, SinglePodTimeout, Namespace)

	pods := kubernetes.GetPods("app=infinispan-pod", Namespace)
	podName := pods[0].Name

	secretName := util.GetSecretName(name)
	expectedClusterSize := 2
	// Check that the cluster size is 2 querying the first pod
	err := wait.Poll(time.Second, TestTimeout, func() (done bool, err error) {
		value, err := cluster.GetClusterSize(secretName, podName, Namespace, protocol)
		if err != nil {
			return false, err
		}
		if value > expectedClusterSize {
			return true, fmt.Errorf("more than expected nodes in cluster (expected=%v, actual=%v)", expectedClusterSize, value)
		}

		return (value == 2), nil
	})

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
		},
	}

	// Register it
	kubernetes.CreateInfinispan(&spec, Namespace)
	defer kubernetes.DeleteInfinispan(&spec, SinglePodTimeout)

	kubernetes.WaitForPods("app=infinispan-pod", 1, SinglePodTimeout, Namespace)

	pass, err := cluster.GetPassword(usr, util.GetSecretName(name), Namespace)
	testutil.ExpectNoError(err)

	routeName := fmt.Sprintf("%s-external", name)
	client := &http.Client{}
	hostAddr := kubernetes.WaitForExternalService(routeName, RouteTimeout, client, Namespace)

	cacheName := "test"
	createCache(cacheName, usr, pass, hostAddr, client)
	defer deleteCache(cacheName, usr, pass, hostAddr, client)

	key := "test"
	value := "test-operator"
	keyURL := fmt.Sprintf("%v/%v", cacheURL(cacheName, hostAddr), key)
	putViaRoute(keyURL, value, client, usr, pass)
	actual := getViaRoute(keyURL, client, usr, pass)

	if actual != value {
		panic(fmt.Errorf("unexpected actual returned: %v (value %v)", actual, value))
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
			Security: ispnv1.InfinispanSecurity{EndpointSecret: "conn-secret-test"},
			Container: ispnv1.InfinispanContainerSpec{
				CPU:    CPU,
				Memory: Memory,
			},
			Image:    getEnvWithDefault("IMAGE", "registry.hub.docker.com/infinispan/server"),
			Replicas: 1,
		},
	}
	kubernetes.CreateInfinispan(&spec, Namespace)
	defer kubernetes.DeleteInfinispan(&spec, SinglePodTimeout)

	kubernetes.WaitForPods("app=infinispan-pod", 1, SinglePodTimeout, Namespace)
	testAuthentication(name, usr, pass)
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
	kubernetes.Kubernetes.Client.Get(context.TODO(), types.NamespacedName{Namespace: spec.Namespace, Name: spec.Name}, &spec)
	spec.Spec.Security.EndpointSecret = "conn-secret-test-1"
	er := kubernetes.Kubernetes.Client.Update(context.TODO(), &spec)
	if er != nil {
		panic(fmt.Errorf(err.Error()))
	}
	kubernetes.WaitForPods("app=infinispan-pod", 1, SinglePodTimeout, Namespace)
	testAuthentication(name, usr, newpass)
}

func testAuthentication(name, usr, pass string) {
	routeName := fmt.Sprintf("%s-external", name)
	client := &http.Client{}
	hostAddr := kubernetes.WaitForExternalService(routeName, RouteTimeout, client, Namespace)

	cacheName := "test"
	createCacheBadCreds(cacheName, "badUser", "badPass", hostAddr, client)
	createCache(cacheName, usr, pass, hostAddr, client)
	defer deleteCache(cacheName, usr, pass, hostAddr, client)

	key := "test"
	value := "test-operator"
	keyURL := fmt.Sprintf("%v/%v", cacheURL(cacheName, hostAddr), key)
	putViaRoute(keyURL, value, client, usr, pass)
	actual := getViaRoute(keyURL, client, usr, pass)

	if actual != value {
		panic(fmt.Errorf("unexpected actual returned: %v (value %v)", actual, value))
	}
}

func cacheURL(cacheName, hostAddr string) string {
	return fmt.Sprintf("http://%v/rest/v2/caches/%s", hostAddr, cacheName)
}

type httpError struct {
	status int
}

func (e *httpError) Error() string {
	return fmt.Sprintf("unexpected response %v", e.status)
}

func createCache(cacheName, usr, pass, hostAddr string, client *http.Client) {
	httpURL := cacheURL(cacheName, hostAddr)
	fmt.Printf("Create cache: %v\n", httpURL)
	httpEmpty(httpURL, "POST", usr, pass, client)
}

func createCacheBadCreds(cacheName, usr, pass, hostAddr string, client *http.Client) {
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
	createCache(cacheName, usr, pass, hostAddr, client)
}

func deleteCache(cacheName, usr, pass, hostAddr string, client *http.Client) {
	httpURL := cacheURL(cacheName, hostAddr)
	fmt.Printf("Delete cache: %v\n", httpURL)
	httpEmpty(httpURL, "DELETE", usr, pass, client)
}

func httpEmpty(httpURL string, method string, usr string, pass string, client *http.Client) {
	req, err := http.NewRequest(method, httpURL, nil)
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
		panic(httpError{resp.StatusCode})
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
	if resp.StatusCode != http.StatusOK {
		panic(httpError{resp.StatusCode})
	}
}
