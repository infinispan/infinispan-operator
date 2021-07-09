package infinispan

import (
	"fmt"

	v1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	ispn "github.com/infinispan/infinispan-operator/pkg/infinispan"
	client "github.com/infinispan/infinispan-operator/pkg/infinispan/client/http"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/http/curl"
	users "github.com/infinispan/infinispan-operator/pkg/infinispan/security"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
)

// Create a cluster for connecting to pods created on the 2.0.x channel
func NewLegacyCluster(i *v1.Infinispan, kubernetes *kube.Kubernetes) (*ispn.Cluster, error) {
	// Use the user secret in order to obtain the old credentials used by the legacy server
	pass, err := users.AdminPassword(i.GetSecretName(), i.Namespace, kubernetes)
	if err != nil {
		return nil, fmt.Errorf("Unable to retrieve identities when creating legacy Cluster instance: %w", err)
	}
	return ispn.NewCluster(consts.DefaultOperatorUser, pass, i.Namespace, i.GetEndpointScheme(), 11222, kubernetes), nil
}

// Create a cluster for connecting to pods created on the 2.1.x channel or above
func NewCluster(i *v1.Infinispan, kubernetes *kube.Kubernetes) (*ispn.Cluster, error) {
	pass, err := users.AdminPassword(i.GetAdminSecretName(), i.Namespace, kubernetes)
	if err != nil {
		return nil, fmt.Errorf("Unable to retrieve operator admin identities when creating Cluster instance: %w", err)
	}
	return ispn.NewCluster(consts.DefaultOperatorUser, pass, i.Namespace, "http", consts.InfinispanAdminPort, kubernetes), nil
}

func NewHttpClient(i *v1.Infinispan, kubernetes *kube.Kubernetes) (client.HttpClient, error) {
	pass, err := users.AdminPassword(i.GetAdminSecretName(), i.Namespace, kubernetes)
	if err != nil {
		return nil, fmt.Errorf("Unable to retrieve opeator admin identities when creating HttpClient instance: %w", err)
	}
	httpConfig := client.HttpConfig{
		Credentials: &client.Credentials{
			Username: consts.DefaultOperatorUser,
			Password: pass,
		},
		Namespace: i.Namespace,
		Protocol:  "http",
		Port:      consts.InfinispanAdminPort,
	}
	return curl.New(httpConfig, kubernetes), nil
}
