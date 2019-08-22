package e2e

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
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
const defaultCliPath = "/opt/jboss/infinispan-server/bin/ispn-cli.sh"

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
			Replicas: 2,
			Image:    getEnvWithDefault("IMAGE", "jboss/infinispan-server:latest"),
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
		value, err := okd.GetClusterSize(Namespace, podName)
		if err != nil {
			return false, err
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
			Replicas: 1,
			Image:    getEnvWithDefault("IMAGE", "jboss/infinispan-server:latest"),
		},
	}

	// Register it
	okd.CreateInfinispan(&spec, Namespace)
	defer okd.DeleteInfinispan("cache-infinispan-0", Namespace, "app=infinispan-pod", SinglePodTimeout)

	err := okd.WaitForPods(Namespace, "app=infinispan-pod", 1, SinglePodTimeout)

	if err != nil {
		panic(err.Error())
	}

	appUser := okd.GetSecret(Namespace, "username", "cache-infinispan-0-app-generated-secret")
	appPass := okd.GetSecret(Namespace, "password", "cache-infinispan-0-app-generated-secret")

	okd.CreateRoute(Namespace, "cache-infinispan-0", 8080, "http")
	defer okd.DeleteRoute(Namespace, "cache-infinispan-0-http")

	client := &http.Client{}
	hostAddr := okd.WaitForRoute(client, Namespace, "cache-infinispan-0-http", RouteTimeout, appUser, appPass)

	value := "test-operator"

	putViaRoute("http://"+hostAddr+"/rest/default/test", value, client, appUser, appPass)
	actual := getViaRoute("http://"+hostAddr+"/rest/default/test", client, appUser, appPass)

	if actual != value {
		panic(fmt.Errorf("unexpected actual returned: %v (value %v)", actual, value))
	}
}

func getEnvVar(env []corev1.EnvVar, name string) string {
	for _, v := range env {
		if v.Name == name {
			return v.Value
		}
	}
	return ""
}

// TestExternalServiceWithAuth starts a cluster and checks application
// and management connection with authentication
func TestExternalServiceWithAuth(t *testing.T) {
	// Create secret with application credentials
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

	// Create secret with management credentials
	mgmtSecret := corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{Name: "mgmt-secret-test"},
		Type:       "Opaque",
		StringData: map[string]string{"username": "connectormgmtusr", "password": "connectormgmtpass"},
	}
	okd.CoreClient().Secrets(Namespace).Create(&mgmtSecret)
	defer okd.CoreClient().Secrets(Namespace).Delete("mgmt-secret-test", &deleteOpts)

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
			Replicas:   1,
			Connector:  ispnv1.InfinispanConnectorInfo{Authentication: ispnv1.InfinispanAuthInfo{Type: "Credentials", SecretName: "conn-secret-test"}},
			Management: ispnv1.InfinispanManagementInfo{Authentication: ispnv1.InfinispanAuthInfo{Type: "Credentials", SecretName: "mgmt-secret-test"}},
			Image:      getEnvWithDefault("IMAGE", "jboss/infinispan-server:latest"),
		},
	}
	okd.CreateInfinispan(&spec, Namespace)
	defer okd.DeleteInfinispan("cache-infinispan-0", Namespace, "app=infinispan-pod", SinglePodTimeout)
	err := okd.WaitForPods(Namespace, "app=infinispan-pod", 1, SinglePodTimeout)

	if err != nil {
		panic(err.Error())
	}

	okd.CreateRoute(Namespace, "cache-infinispan-0", 8080, "http")
	defer okd.DeleteRoute(Namespace, "cache-infinispan-0-http")

	okd.CreateRoute(Namespace, "cache-infinispan-0", 9990, "mgmt")
	defer okd.DeleteRoute(Namespace, "cache-infinispan-0-mgmt")

	client := &http.Client{}
	hostAddr := okd.WaitForRoute(client, Namespace, "cache-infinispan-0-http", RouteTimeout, "connectorusr", "connectorpass")
	mgmtEnabled := os.Getenv("TEST_MGMT_ENABLED")
	value := "test-operator"

	putViaRoute("http://"+hostAddr+"/rest/default/test", value, client, "connectorusr", "connectorpass")
	actual := getViaRoute("http://"+hostAddr+"/rest/default/test", client, "connectorusr", "connectorpass")
	if actual != value {
		panic(fmt.Errorf("unexpected actual returned: %v (value %v)", actual, value))
	}

	if mgmtEnabled != "false" {
		hostAddrMgmt := okd.WaitForRoute(client, Namespace, "cache-infinispan-0-mgmt", RouteTimeout, "connectorusr", "connectorpass")
		mgmtConnectViaRoute("http://"+hostAddrMgmt+"/management", value, client, "connectormgmtusr", "connectormgmtpass")
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

// mgmtConnectViaRoute tests the connection to the management interface
// authenticating with digest-md5
func mgmtConnectViaRoute(url string, value string, client *http.Client, user string, pass string) {
	body := bytes.NewBuffer([]byte(value))
	req, err := http.NewRequest("GET", url, body)
	resp, err := client.Do(req)
	if err != nil {
		panic(err.Error())
	}
	if resp.StatusCode != http.StatusUnauthorized {
		panic(fmt.Errorf("unexpected response %v", resp))
	}

	digestParts := map[string]string{}
	digestParts["nonce"] = getNonce(resp)
	digestParts["realm"] = "ManagementRealm"
	digestParts["qop"] = "auth"
	digestParts["uri"] = url
	digestParts["method"] = "GET"
	digestParts["username"] = user
	digestParts["password"] = pass
	req, err = http.NewRequest("GET", url, body)
	req.Header.Set("Authorization", getDigestAuthrization(digestParts))
	resp, err = client.Do(req)
	if err != nil {
		panic(err.Error())
	}
	if resp.StatusCode != http.StatusOK {
		panic(fmt.Errorf("unexpected response %v", resp))
	}
}

func getNonce(resp *http.Response) string {
	if len(resp.Header["Www-Authenticate"]) > 0 {
		responseHeaders := strings.Split(resp.Header["Www-Authenticate"][0], ",")
		for _, r := range responseHeaders {
			if strings.Contains(r, "nonce") {
				return strings.Split(r, `"`)[1]
			}
		}
	}
	return ""
}

func getMD5(text string) string {
	hasher := md5.New()
	hasher.Write([]byte(text))
	return hex.EncodeToString(hasher.Sum(nil))
}

func getCnonce() string {
	b := make([]byte, 8)
	io.ReadFull(rand.Reader, b)
	return fmt.Sprintf("%x", b)[:16]
}

func getDigestAuthrization(digestParts map[string]string) string {
	d := digestParts
	ha1 := getMD5(d["username"] + ":" + d["realm"] + ":" + d["password"])
	ha2 := getMD5(d["method"] + ":" + d["uri"])
	nonceCount := 00000001
	cnonce := getCnonce()
	response := getMD5(fmt.Sprintf("%s:%s:%v:%s:%s:%s", ha1, d["nonce"], nonceCount, cnonce, d["qop"], ha2))
	authorization := fmt.Sprintf(`Digest username="%s", realm="%s", nonce="%s", uri="%s", cnonce="%s", nc="%v", qop="%s", response="%s"`,
		d["username"], d["realm"], d["nonce"], d["uri"], cnonce, nonceCount, d["qop"], response)
	return authorization
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
			Replicas: 2,
			Image:    getEnvWithDefault("IMAGE", "jboss/infinispan-server:latest"),
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
		value, err := okd.GetClusterSize(Namespace, podName)
		if err != nil {
			return false, err
		}
		return (value == 2), nil
	})

	if err != nil {
		panic(err.Error())
	}

}
