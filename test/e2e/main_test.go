package e2e

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	"github.com/infinispan/infinispan-operator/test/e2e/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func getConfigLocation() string {
	kubeConfig := os.Getenv("KUBECONFIG")
	if kubeConfig != "" {
		return kubeConfig
	} else {
		return "../../openshift.local.clusterup/kube-apiserver/admin.kubeconfig"
	}
}

var ConfigLocation = getConfigLocation()

const Namespace = "namespace-for-testing"
const TestTimeout = 5 * time.Minute
const SinglePodTimeout = 5 * time.Minute
const RouteTimeout = 10 * time.Second

var okd = util.NewOKDClient(ConfigLocation)

func TestMain(m *testing.M) {
	namespace := strings.ToLower(Namespace)
	okd.NewProject(namespace)
	stopCh := util.RunOperator(okd, Namespace, ConfigLocation)
	code := m.Run()
	util.Cleanup(*okd, Namespace, stopCh)
	os.Exit(code)
}

// Simple smoke test to check if the OKD is alive
func TestSimple(t *testing.T) {
	okd := util.NewOKDClient(ConfigLocation)
	fmt.Printf("%v\n", okd.Nodes())
	fmt.Printf("%s\n", okd.WhoAmI())
	fmt.Printf("%s\n", okd.Pods("default", ""))
}

func TestExternalService(t *testing.T) {
	// Create a resource without passing any config
	spec := ispnv1.Infinispan{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v1",
			Kind:       "Infinispan",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "cache-infinispan-0",
		},
		Spec: ispnv1.InfinispanSpec{
			Size: 1,
		},
	}

	// Register it
	okd.CreateInfinispan(&spec, Namespace)
	defer okd.DeleteInfinispan("cache-infinispan-0", Namespace)

	err := okd.WaitForPods(Namespace, "app=infinispan-pod", 1, SinglePodTimeout)

	if err != nil {
		panic(err.Error())
	}

	route := okd.CreateRoute(Namespace, "cache-infinispan-0", "http")
	defer okd.DeleteRoute("cache-infinispan-0", Namespace)

	url := "http://" + route.Spec.Host + "/rest/default/test"
	client := &http.Client{}
	okd.WaitForRoute(url, client, RouteTimeout)

	value := "test-operator"

	putViaRoute(url, value, client)
	actual := getViaRoute(url, client)

	if actual != value {
		panic(fmt.Errorf("unexpected actual returned: %v (value %v)", actual, value))
	}
}

func getViaRoute(url string, client *http.Client) string {
	req, err := http.NewRequest("GET", url, nil)
	req.SetBasicAuth("infinispan", "infinispan")
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

func putViaRoute(url string, value string, client *http.Client) {
	body := bytes.NewBuffer([]byte(value))
	req, err := http.NewRequest("POST", url, body)
	req.Header.Set("Content-Type", "text/plain")
	req.SetBasicAuth("infinispan", "infinispan")
	fmt.Printf("Put request via route: %v\n", req)
	resp, err := client.Do(req)
	if err != nil {
		panic(err.Error())
	}
	if resp.StatusCode != http.StatusOK {
		panic(fmt.Errorf("unexpected response %v", resp))
	}
}

// Test for operator installation and creation of a cluster, using configuration from the config map
func TestCreateClusterWithConfigMap(t *testing.T) {
	// Install config map from deploy folder
	configMapName := "test-config-map"
	util.InstallConfigMap(Namespace, configMapName, okd)

	// Create a resource using external config from a ConfigMap
	spec := ispnv1.Infinispan{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v1",
			Kind:       "Infinispan",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "cache-infinispan",
		},
		Config: ispnv1.InfinispanConfig{
			SourceType: ispnv1.ConfigMap,
			SourceRef:  configMapName,
			Name:       "cloud-ephemeral.xml",
		},
		Spec: ispnv1.InfinispanSpec{
			Size: 2,
		},
	}

	// Register it
	okd.CreateInfinispan(&spec, Namespace)
	defer okd.DeleteInfinispan("cache-infinispan", Namespace)

	// Make sure 2 pods are started
	err := okd.WaitForPods(Namespace, "app=infinispan-pod", 2, TestTimeout)

	if err != nil {
		panic(err.Error())
	}

}

func TestCreateWithInternalConfig(t *testing.T) {
	// Create a cluster with a pre-canned config
	spec := ispnv1.Infinispan{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v1",
			Kind:       "Infinispan",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "cache-infinispan-precanned-config",
		},
		Config: ispnv1.InfinispanConfig{
			SourceType: ispnv1.Internal,
			Name:       "clustered.xml",
		},
		Spec: ispnv1.InfinispanSpec{
			Size: 2,
		},
	}

	// Register it
	okd.CreateInfinispan(&spec, Namespace)
	// Cleanup resource
	defer okd.DeleteInfinispan("cache-infinispan-precanned-config", Namespace)

	// Make sure 2 pods are started
	err := okd.WaitForPods(Namespace, "clusterName=cache-infinispan-precanned-config", 2, TestTimeout)

	if err != nil {
		panic(err.Error())
	}
}
