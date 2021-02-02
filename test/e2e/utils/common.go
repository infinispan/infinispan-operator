package utils

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	ispn "github.com/infinispan/infinispan-operator/pkg/infinispan"
	users "github.com/infinispan/infinispan-operator/pkg/infinispan/security"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
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
func NewCluster(user, secret, protocol, namespace string, kubernetes *kube.Kubernetes) *ispn.Cluster {
	pass, _ := users.PasswordFromSecret(user, secret, namespace, kubernetes)
	return ispn.NewCluster(user, pass, namespace, protocol, kubernetes)
}

