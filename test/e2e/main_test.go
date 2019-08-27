package e2e

import (
	"bytes"
	"fmt"
	"github.com/infinispan/infinispan-operator/pkg/controller/infinispan/util"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	utilk8s "github.com/infinispan/infinispan-operator/test/e2e/util/k8s"
	corev1 "k8s.io/api/core/v1"
	apiv1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

func getConfigLocation() string {
	kubeConfig := os.Getenv("KUBECONFIG")
	if kubeConfig != "" {
		return kubeConfig
	}
	return "../../openshift.local.clusterup/kube-apiserver/admin.kubeconfig"
}

var ConfigLocation = getConfigLocation()

const Namespace = "namespace-for-testing"
const TestTimeout = 5 * time.Minute
const SinglePodTimeout = 5 * time.Minute
const RouteTimeout = 240 * time.Second

var Cpu = getEnvWithDefault("INFINISPAN_CPU", "0.5")
var Memory = getEnvWithDefault("INFINISPAN_MEMORY", "512Mi")

// Options used when deleting resources
var deletePropagation = apiv1.DeletePropagationBackground
var gracePeriod = int64(0)
var deleteOpts = apiv1.DeleteOptions{PropagationPolicy: &deletePropagation, GracePeriodSeconds: &gracePeriod}

var okd = utilk8s.NewK8sClient(ConfigLocation)

func TestMain(m *testing.M) {
	namespace := strings.ToLower(Namespace)
	okd.NewProject(namespace)
	stopCh := utilk8s.RunOperator(okd, Namespace, ConfigLocation)
	setupNamespace()
	code := m.Run()
	cleanupNamespace()
	utilk8s.Cleanup(*okd, Namespace, stopCh)
	os.Exit(code)
}

func setupNamespace() {
	volumeSecretName, defined := os.LookupEnv("VOLUME_SECRET_NAME")
	if defined {
		secret := corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "v1",
				Kind:       "Secret",
			},
			ObjectMeta: metav1.ObjectMeta{Name: volumeSecretName},
			Type:       "Opaque",
			StringData: map[string]string{"username": "connectorusr", "password": "connectorpass"},
		}
		okd.CoreClient().Secrets(Namespace).Create(&secret)
	}
}

func cleanupNamespace() {
	volumeSecretName, defined := os.LookupEnv("VOLUME_SECRET_NAME")
	if defined {
		okd.CoreClient().Secrets(Namespace).Delete(volumeSecretName, &deleteOpts)
	}
}

// Simple smoke test to check if the OKD is alive
func TestSimple(t *testing.T) {
	okd := utilk8s.NewK8sClient(ConfigLocation)
	fmt.Printf("%v\n", okd.Nodes())
	fmt.Printf("%s\n", okd.Pods("default", ""))
	fmt.Printf("%s\n", okd.PublicIp())
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
				CPU:    Cpu,
				Memory: Memory,
			},
			Image:    getEnvWithDefault("IMAGE", "quay.io/remerson/server"),
			Replicas: 2,
		},
	}
	// Register it
	okd.CreateInfinispan(&spec, Namespace)
	defer okd.DeleteInfinispan(name, Namespace, "app=infinispan-pod", SinglePodTimeout)

	// Wait that 2 pods are up
	err := okd.WaitForPods(Namespace, "app=infinispan-pod", 2, SinglePodTimeout)
	if err != nil {
		panic(err.Error())
	}

	pods, err := okd.GetPods(Namespace, "app=infinispan-pod")
	if err != nil {
		panic(err.Error())
	}
	podName := pods[0].Name

	secretName := util.GetSecretName(name)
	expectedClusterSize := 2
	// Check that the cluster size is 2 querying the first pod
	err = wait.Poll(time.Second, TestTimeout, func() (done bool, err error) {
		value, err := util.GetClusterSize(secretName, podName, Namespace)
		if err != nil {
			return false, err
		}
		if value > expectedClusterSize {
			return true, fmt.Errorf("more than expected nodes in cluster (expected=%v, actual=%v)", expectedClusterSize, value)
		}

		return (value == 2), nil
	})

	if err != nil {
		panic(err.Error())
	}

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
				CPU:    Cpu,
				Memory: Memory,
			},
			Image:    getEnvWithDefault("IMAGE", "quay.io/remerson/server"),
			Replicas: 1,
		},
	}

	// Register it
	okd.CreateInfinispan(&spec, Namespace)
	defer okd.DeleteInfinispan(name, Namespace, "app=infinispan-pod", SinglePodTimeout)

	err := okd.WaitForPods(Namespace, "app=infinispan-pod", 1, SinglePodTimeout)
	ExpectNoError(err)

	pass, err := util.GetPassword(usr, util.GetSecretName(name), Namespace)
	ExpectNoError(err)

	routeName := fmt.Sprintf("%s-external", name)
	okd.CreateRoute(Namespace, name, 11222, routeName)
	defer okd.DeleteRoute(Namespace, routeName)
	client := &http.Client{}
	hostAddr := okd.WaitForRoute(client, Namespace, routeName, RouteTimeout)

	cacheName := "test"
	createCache(cacheName, usr, pass, hostAddr, client)
	defer deleteCache(cacheName, usr, pass, hostAddr, client)

	key := "test"
	value := "test-operator"
	keyUrl := fmt.Sprintf("%v/%v", cacheUrl(cacheName, hostAddr), key)
	putViaRoute(keyUrl, value, client, usr, pass)
	actual := getViaRoute(keyUrl, client, usr, pass)

	if actual != value {
		panic(fmt.Errorf("unexpected actual returned: %v (value %v)", actual, value))
	}
}

// TestExternalServiceWithAuth starts a cluster and checks application
// and management connection with authentication
func TestExternalServiceWithAuth(t *testing.T) {
	usr := "connectorusr"
	pass := "connectorpass"

	identities := util.CreateIdentitiesFor(usr, pass)
	yaml, err := util.ToYaml(&identities)
	ExpectNoError(err)

	// Create secret with application credentials
	secret := corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "conn-secret-test"},
		Type:       "Opaque",
		StringData: map[string]string{"identities.yaml": string(yaml)},
	}
	okd.CoreClient().Secrets(Namespace).Create(&secret)
	defer okd.CoreClient().Secrets(Namespace).Delete("conn-secret-test", &deleteOpts)

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
				CPU:    Cpu,
				Memory: Memory,
			},
			Image:    getEnvWithDefault("IMAGE", "quay.io/remerson/server"),
			Replicas: 1,
		},
	}
	okd.CreateInfinispan(&spec, Namespace)
	defer okd.DeleteInfinispan(name, Namespace, "app=infinispan-pod", SinglePodTimeout)
	err = okd.WaitForPods(Namespace, "app=infinispan-pod", 1, SinglePodTimeout)
	ExpectNoError(err)

	routeName := fmt.Sprintf("%s-external", name)
	okd.CreateRoute(Namespace, name, 11222, routeName)
	defer okd.DeleteRoute(Namespace, routeName)

	client := &http.Client{}
	hostAddr := okd.WaitForRoute(client, Namespace, routeName, RouteTimeout)

	cacheName := "test"
	createCache(cacheName, usr, pass, hostAddr, client)
	defer deleteCache(cacheName, usr, pass, hostAddr, client)

	key := "test"
	value := "test-operator"
	keyUrl := fmt.Sprintf("%v/%v", cacheUrl(cacheName, hostAddr), key)
	putViaRoute(keyUrl, value, client, usr, pass)
	actual := getViaRoute(keyUrl, client, usr, pass)

	if actual != value {
		panic(fmt.Errorf("unexpected actual returned: %v (value %v)", actual, value))
	}
}

func cacheUrl(cacheName, hostAddr string) string {
	return fmt.Sprintf("http://%v/rest/v2/caches/%s", hostAddr, cacheName)
}

func createCache(cacheName, usr, pass, hostAddr string, client *http.Client) {
	httpUrl := cacheUrl(cacheName, hostAddr)
	fmt.Printf("Create cache: %v\n", httpUrl)
	httpEmpty(httpUrl, "POST", usr, pass, client)
}

func deleteCache(cacheName, usr, pass, hostAddr string, client *http.Client) {
	httpUrl := cacheUrl(cacheName, hostAddr)
	fmt.Printf("Delete cache: %v\n", httpUrl)
	httpEmpty(httpUrl, "DELETE", usr, pass, client)
}

func httpEmpty(httpUrl string, method string, usr string, pass string, client *http.Client) {
	req, err := http.NewRequest(method, httpUrl, nil)
	ExpectNoError(err)

	req.SetBasicAuth(usr, pass)
	resp, err := client.Do(req)
	ExpectNoError(err)

	if resp.StatusCode != http.StatusOK {
		panic(fmt.Errorf("unexpected response %v", resp))
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
		panic(fmt.Errorf("unexpected response %v", resp))
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
		panic(fmt.Errorf("unexpected response %v", resp))
	}
}

func ExpectNoError(err error) {
	if err != nil {
		panic(err.Error())
	}
}
