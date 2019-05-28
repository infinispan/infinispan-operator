package e2e

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	corev1 "k8s.io/api/core/v1"
	apiv1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilk8s "github.com/infinispan/infinispan-operator/test/e2e/util/k8s"
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
const RouteTimeout = 60 * time.Second

// Options used when deleting resources
var deletePropagation = apiv1.DeletePropagationBackground
var gracePeriod = int64(0)
var deleteOpts = apiv1.DeleteOptions{PropagationPolicy: &deletePropagation, GracePeriodSeconds: &gracePeriod}

var okd = utilk8s.NewK8sClient(ConfigLocation)

func TestMain(m *testing.M) {
	namespace := strings.ToLower(Namespace)
	okd.NewProject(namespace)
	stopCh := utilk8s.RunOperator(okd, Namespace, ConfigLocation)
	code := m.Run()
	utilk8s.Cleanup(*okd, Namespace, stopCh)
	os.Exit(code)
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
	spec := ispnv1.Infinispan{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v1",
			Kind:       "Infinispan",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "cache-infinispan",
		},
		Spec: ispnv1.InfinispanSpec{
			Size: 2,
		},
	}
	// Register it
	okd.CreateInfinispan(&spec, Namespace)
	defer okd.DeleteInfinispan("cache-infinispan", Namespace, "app=infinispan-pod", SinglePodTimeout)

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

	// Check that the cluster size is 2 querying the first pod
	err = wait.Poll(time.Second, TestTimeout, func() (done bool, err error) {
		value, err := getClusterSize(Namespace, podName)
		if err != nil {
			return false, err
		}
		return (value == 2), nil
	})

	if err != nil {
		panic(err.Error())
	}

}

// This function get the cluster size via the ISPN cli
func getClusterSize(namespace, namePod string) (int, error) {
	cliCommand := "/subsystem=datagrid-infinispan/cache-container=clustered/:read-attribute(name=cluster-size)\n"
	commands := []string{"/opt/jboss/infinispan-server/bin/ispn-cli.sh", "--connect"}
	var execIn, execOut, execErr bytes.Buffer
	execIn.WriteString(cliCommand)
	err := okd.ExecuteCmdOnPod(namespace, namePod, commands,
		&execIn, &execOut, &execErr)
	if err == nil {
		result := execOut.String()
		// Match the correct line in the output
		resultRegExp := regexp.MustCompile("\"result\" => \"\\d+\"")
		// Match the result value
		valueRegExp := regexp.MustCompile("\\d+")
		resultLine := resultRegExp.FindString(result)
		resultValueStr := valueRegExp.FindString(resultLine)
		return strconv.Atoi(resultValueStr)
	}
	return 0, err
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
	defer okd.DeleteInfinispan("cache-infinispan-0", Namespace, "app=infinispan-pod", SinglePodTimeout)

	err := okd.WaitForPods(Namespace, "app=infinispan-pod", 1, SinglePodTimeout)

	if err != nil {
		panic(err.Error())
	}

	nodePort := okd.CreateRoute(Namespace, "cache-infinispan-0", "http")
	defer okd.DeleteRoute(Namespace, "cache-infinispan-0")

	host := okd.PublicIp()
	url := "http://" + host + ":" + fmt.Sprint(nodePort) + "/rest/default/test"
	client := &http.Client{}
	okd.WaitForRoute(url, client, RouteTimeout, "infinispan", "infinispan")

	value := "test-operator"

	putViaRoute(url, value, client, "infinispan", "infinispan")
	actual := getViaRoute(url, client, "infinispan", "infinispan")

	if actual != value {
		panic(fmt.Errorf("unexpected actual returned: %v (value %v)", actual, value))
	}
}

func TestExternalServiceWithAuth(t *testing.T) {
	// Create secret with credentials
	secret := corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "conn-secret-test"},
		Type:       "Opaque",
		StringData: map[string]string{"username": "connectorusr", "password": "connectorpass"},
	}
	okd.CoreClient().Secrets(Namespace).Create(&secret)
	defer okd.CoreClient().Secrets(Namespace).Delete("conn-secret-test", &deleteOpts)

	// Create Infinispan
	spec := ispnv1.Infinispan{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v1",
			Kind:       "Infinispan",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "cache-infinispan-0",
		},
		Spec: ispnv1.InfinispanSpec{
			Size:      1,
			Connector: ispnv1.InfinispanConnectorInfo{Authentication: ispnv1.InfinispanAuthInfo{Secret: ispnv1.InfinispanSecret{Type: "Credentials", SecretName: "conn-secret-test"}}},
		},
	}
	okd.CreateInfinispan(&spec, Namespace)
	defer okd.DeleteInfinispan("cache-infinispan-0", Namespace, "app=infinispan-pod", SinglePodTimeout)
	err := okd.WaitForPods(Namespace, "app=infinispan-pod", 1, SinglePodTimeout)

	if err != nil {
		panic(err.Error())
	}

	nodePort := okd.CreateRoute(Namespace, "cache-infinispan-0", "http")
	defer okd.DeleteRoute("cache-infinispan-0", Namespace)

	host := okd.PublicIp()
	url := "http://" + host + ":" + fmt.Sprint(nodePort) + "/rest/default/test"
	client := &http.Client{}
	okd.WaitForRoute(url, client, RouteTimeout, "connectorusr", "connectorpass")

	value := "test-operator"

	putViaRoute(url, value, client, "connectorusr", "connectorpass")
	actual := getViaRoute(url, client, "connectorusr", "connectorpass")

	if actual != value {
		panic(fmt.Errorf("unexpected actual returned: %v (value %v)", actual, value))
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

// Test for operator installation and creation of a cluster, using configuration from the config map
func TestCreateClusterWithConfigMap(t *testing.T) {
	// Install config map from deploy folder
	configMapName := "test-config-map"
	utilk8s.InstallConfigMap(Namespace, configMapName, okd)

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
	defer okd.DeleteInfinispan("cache-infinispan", Namespace, "app=infinispan-pod", SinglePodTimeout)

	// Make sure 2 pods are started
	err := okd.WaitForPods(Namespace, "app=infinispan-pod", 2, TestTimeout)

	pods, err := okd.GetPods(Namespace, "app=infinispan-pod")
	if err != nil {
		panic(err.Error())
	}
	podName := pods[0].Name

	// Check that the cluster size is 2 querying the first pod
	err = wait.Poll(time.Second, TestTimeout, func() (done bool, err error) {
		value, err := getClusterSize(Namespace, podName)
		if err != nil {
			return false, err
		}
		return (value == 2), nil
	})

	if err != nil {
		panic(err.Error())
	}

}
