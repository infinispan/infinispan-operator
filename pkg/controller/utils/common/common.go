package common

import (
	"net"
	"os"

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

func LookupHost(host string, logger logr.Logger) (string, error) {
	addresses, err := net.LookupHost(host)
	if err != nil {
		logger.Error(err, "host does not resolve")
		return "", err
	}
	logger.Info("host resolved", "host", host, "addresses", addresses)
	return host, nil
}

// TODO Replace with controllerutil.ContainsFinalizer after operator-sdk update (controller-runtime v0.6.1+)
func Contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// TODO Replace with controllerutil.RemoveFinalizer after operator-sdk update (controller-runtime v0.6.0+)
func Remove(list []string, s string) []string {
	var slice []string
	for _, v := range list {
		if v != s {
			slice = append(slice, v)
		}
	}
	return slice
}
