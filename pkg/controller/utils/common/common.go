package common

import (
	"net"
	"os"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
)

// GetEnvWithDefault return GetEnv(name) if exists else return defVal
func GetEnvWithDefault(name, defVal string) string {
	str := os.Getenv(name)
	if str != "" {
		return str
	}
	return defVal
}

func GetEnvVarIndex(envVarName string, env *[]corev1.EnvVar) int {
	for i, value := range *env {
		if value.Name == envVarName {
			return i
		}
	}
	return 0
}

func ToMilliDecimalQuantity(value int64) resource.Quantity {
	return *resource.NewMilliQuantity(value, resource.DecimalSI)
}

func LookupHost(host string, logger logr.Logger) (string, error) {
	addresses, err := net.LookupHost(host)
	if err != nil {
		logger.Error(err, "host does not resolve")
		return "", err
	}
	logger.Info("host resolved", "host", host, "addresses", addresses)
	return host, nil
}

// IsGroupVersionSupported checks if groupVersion Kind is supported by the platform
func IsGroupVersionSupported(groupVersion string, kind string, restConfig *rest.Config, log logr.Logger) (bool, error) {
	// TODO: Consider to save the DiscoverClient in the Reconcile struct, if performance is critical
	cli, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		log.Error(err, "Failed to return a discovery client for the current reconciler")
		return false, err
	}

	res, err := cli.ServerResourcesForGroupVersion(groupVersion)
	if err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	for _, v := range res.APIResources {
		if v.Kind == kind {
			return true, nil
		}
	}

	return false, nil
}
