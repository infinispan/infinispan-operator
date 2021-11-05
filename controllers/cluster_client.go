package controllers

import (
	"context"
	"fmt"

	v1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	ispn "github.com/infinispan/infinispan-operator/pkg/infinispan"
	users "github.com/infinispan/infinispan-operator/pkg/infinispan/security"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
)

func NewCluster(i *v1.Infinispan, kubernetes *kube.Kubernetes, ctx context.Context) (*ispn.Cluster, error) {
	pass, err := users.AdminPassword(i.GetAdminSecretName(), i.Namespace, kubernetes, ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve operator admin identities when creating Cluster instance: %w", err)
	}
	return ispn.NewCluster(consts.DefaultOperatorUser, pass, i.Namespace, "http", kubernetes), nil
}
