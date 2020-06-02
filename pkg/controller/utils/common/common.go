package common

import (
	"net"
	"os"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
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

func GetURISchemeProtocol(URIScheme corev1.URIScheme) string {
	return strings.ToLower(string(URIScheme))
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
