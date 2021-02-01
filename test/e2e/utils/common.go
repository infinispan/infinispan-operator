package utils

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	v1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	"github.com/infinispan/infinispan-operator/pkg/controller/constants"
	ispn "github.com/infinispan/infinispan-operator/pkg/infinispan"
	users "github.com/infinispan/infinispan-operator/pkg/infinispan/security"
	"k8s.io/apimachinery/pkg/util/yaml"
)

func GetYamlReaderFromFile(filename string) (*yaml.YAMLReader, error) {
	absFileName := getAbsolutePath(filename)
	f, err := os.Open(absFileName)
	if err != nil {
		return nil, err
	}
	return yaml.NewYAMLReader(bufio.NewReader(f)), nil
}

// Obtain the file absolute path given a relative path
func getAbsolutePath(relativeFilePath string) string {
	if !strings.HasPrefix(relativeFilePath, ".") {
		return relativeFilePath
	}
	dir, _ := os.Getwd()
	absPath, _ := filepath.Abs(dir + "/" + relativeFilePath)
	return absPath
}

// NewCluster returns a new infinispan.Cluster ready to use
func NewCluster(infinispan *v1.Infinispan, kube *TestKubernetes) *ispn.Cluster {
	k8s := kube.Kubernetes
	namespace := infinispan.Namespace
	protocol := kube.GetSchemaForRest(infinispan)

	if !infinispan.IsAuthenticationEnabled() {
		return ispn.NewClusterNoAuth(namespace, protocol, k8s)
	}

	user := constants.DefaultOperatorUser
	secret := infinispan.GetSecretName()
	pass, _ := users.PasswordFromSecret(user, secret, namespace, k8s)
	return ispn.NewCluster(user, pass, namespace, protocol, k8s)
}
