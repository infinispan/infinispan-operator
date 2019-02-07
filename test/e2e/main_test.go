package e2e

import (
	"fmt"
	ispnv1 "github.com/jboss-dockerfiles/infinispan-server-operator/pkg/apis/infinispan/v1"
	"github.com/jboss-dockerfiles/infinispan-server-operator/test/e2e/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"os"
	"strings"
	"testing"
	"time"
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
			Size:        2,
			ClusterName: "helloworldcluster",
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
	// Create a resource without passing any config
	spec := ispnv1.Infinispan{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v1",
			Kind:       "Infinispan",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "cache-infinispan-1",
		},
		Spec: ispnv1.InfinispanSpec{
			Size:        2,
			ClusterName: "minimal",
		},
	}

	// Register it
	okd.CreateInfinispan(&spec, Namespace)

	// Make sure 2 pods are started
	err := okd.WaitForPods(Namespace, "clusterName=minimal", 2, TestTimeout)

	// Cleanup resource
	defer okd.DeleteInfinispan("cache-infinispan-1", Namespace)

	if err != nil {
		panic(err.Error())
	}

	// Create another cluster with a pre-canned config
	resourceName := "cache-infinispan-2"
	spec = ispnv1.Infinispan{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v1",
			Kind:       "Infinispan",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: resourceName,
		},
		Config: ispnv1.InfinispanConfig{
			SourceType: ispnv1.Internal,
			Name:       "clustered.xml",
		},
		Spec: ispnv1.InfinispanSpec{
			Size:        2,
			ClusterName: "pre-canned-config",
		},
	}

	// Register it
	okd.CreateInfinispan(&spec, Namespace)
	// Cleanup resource
	defer okd.DeleteInfinispan(resourceName, Namespace)

	// Make sure 2 pods are started
	err = okd.WaitForPods(Namespace, "clusterName=pre-canned-config", 2, TestTimeout)

	if err != nil {
		panic(err.Error())
	}
}
