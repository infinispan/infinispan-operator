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

func NewCluster(i *v1.Infinispan, kubernetes *kube.Kubernetes) (*ispn.Cluster, error) {
	pass, err := users.AdminPassword(i.GetAdminSecretName(), i.Namespace, kubernetes)
	if err != nil {
		return nil, fmt.Errorf("Unable to retrieve opeator admin identities when creating Cluster instance: %w", err)
	}
	return ispn.NewCluster(consts.DefaultOperatorUser, pass, i.Namespace, "http", kubernetes), nil
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
	}
	return curl.New(httpConfig, kubernetes), nil
}
