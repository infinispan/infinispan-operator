package e2e

import (
	"fmt"
	"github.com/jboss-dockerfiles/infinispan-server-operator/pkg/apis/infinispan/v1"
	"github.com/jboss-dockerfiles/infinispan-server-operator/test/e2e/util"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
	"time"
)

const ConfigLocation = "../../openshift.local.clusterup/kube-apiserver/admin.kubeconfig"

// Simple smoke test to check if the OKD is alive
func TestSimple(t *testing.T) {
	okd := util.NewOKDClient(ConfigLocation)
	fmt.Printf("%v\n", okd.Nodes())
	fmt.Printf("%s\n", okd.WhoAmI())
	fmt.Printf("%s\n", okd.Pods("default", ""))
}

// Test for operator installation and creation of a cluster
func TestCreateCluster(t *testing.T) {
	// Client OKD client
	okd := util.NewOKDClient(ConfigLocation)

	// Create a namespace for the test
	namespace := "ispn-operator-test"
	okd.NewProject(namespace)

	// Run operator locally
	util.RunOperator(okd, namespace, ConfigLocation)

	// Cleanup when done
	defer util.Cleanup(*okd, namespace)

	// Create a resource
	spec := v1.Infinispan{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "infinispan.org/v1",
			Kind:       "Infinispan",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "cache-infinispan",
		},
		Spec: v1.InfinispanSpec{
			Size:        2,
			ClusterName: "helloworldcluster",
		},
	}

	// Register it
	okd.CreateInfinispan(&spec, namespace)

	// Make sure 2 pods are started
	err := okd.WaitForPods(namespace, "app=infinispan-pod", 2, 2*time.Minute)

	if err != nil {
		panic(err.Error())
	}

}
