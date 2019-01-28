package util

import (
	"bufio"
	"github.com/jboss-dockerfiles/infinispan-server-operator/pkg/launcher"
	osv1 "github.com/openshift/api/authorization/v1"
	"k8s.io/api/core/v1"
	apiextv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/util/yaml"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Provides methods to install and uninstall the operator and dependencies.
func RunOperator(okd *ExternalOKD, ns, config string) {
	installConfigMap(ns, okd)
	installRBAC(ns, okd)
	installCRD(okd)
	go runOperatorLocally(ns, config)
}

func Cleanup(client ExternalOKD, namespace string) {
	StopOperator()
	client.DeleteProject(namespace)
	client.DeleteCRD("infinispans.infinispan.org")
}

func installConfigMap(ns string, okd *ExternalOKD) {
	dir := GetRelativeDir("../../config/cloud-ephemeral.xml")
	okd.CreateOrUpdateConfigMap("infinispan-app-configuration", dir, ns)
}

// Install resources from rbac.yaml required by the Infinispan operator
func installRBAC(ns string, okd *ExternalOKD) {
	filename := GetRelativeDir("../../deploy/rbac.yaml")
	f, err := os.Open(filename)
	if err != nil {
		panic(err)
	}
	yamlReader := yaml.NewYAMLReader(bufio.NewReader(f))

	read, _ := yamlReader.Read()
	role := osv1.Role{}
	err = yaml.NewYAMLToJSONDecoder(strings.NewReader(string(read))).Decode(&role)
	okd.CreateOrUpdateRole(&role, ns)

	read2, _ := yamlReader.Read()
	sa := v1.ServiceAccount{}
	err = yaml.NewYAMLToJSONDecoder(strings.NewReader(string(read2))).Decode(&sa)
	okd.CreateOrUpdateSa(&sa, ns)

	read3, _ := yamlReader.Read()
	binding := osv1.RoleBinding{}
	err = yaml.NewYAMLToJSONDecoder(strings.NewReader(string(read3))).Decode(&binding)
	okd.CreateOrUpdateRoleBinding(&binding, ns)
}

func installCRD(okd *ExternalOKD) {
	filename := GetRelativeDir("../../deploy/crd.yaml")
	f, err := os.Open(filename)
	if err != nil {
		panic(err)
	}
	yamlReader := yaml.NewYAMLReader(bufio.NewReader(f))

	read, _ := yamlReader.Read()
	crd := apiextv1beta1.CustomResourceDefinition{}
	err = yaml.NewYAMLToJSONDecoder(strings.NewReader(string(read))).Decode(&crd)
	okd.CreateAndWaitForCRD(&crd)
}

// Run the operator locally
func runOperatorLocally(ns string, configLocation string) {
	_ = os.Setenv("WATCH_NAMESPACE", ns)
	_ = os.Setenv("KUBECONFIG", configLocation)
	launcher.Launch()
}

func StopOperator() {
	_ = syscall.Kill(syscall.Getpid(), syscall.SIGINT)
}

func GetRelativeDir(path string) string {
	dir, _ := os.Getwd()
	absPath, _ := filepath.Abs(dir + "/" + path)
	return absPath
}
