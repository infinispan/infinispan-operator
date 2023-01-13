package controllers

import (
	"context"
	"fmt"

	v1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/http/curl"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
	users "github.com/infinispan/infinispan-operator/pkg/infinispan/security"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	. "github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan/handler/provision"
)

// NewInfinispan returns a new api.Infinispan client using the first pod in the cluster's StatefulSet
func NewInfinispan(ctx context.Context, i *v1.Infinispan, versionManager *version.Manager, kubernetes *kube.Kubernetes) (api.Infinispan, error) {
	podList, err := PodsCreatedBy(i.Namespace, kubernetes, ctx, i.GetStatefulSetName())
	if err != nil {
		return nil, err
	}

	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("no Infinispan pods exist")
	}
	return NewInfinispanForPod(ctx, podList.Items[0].Name, i, versionManager, kubernetes)
}

// NewInfinispanForPod retrieves credential information to initialise a curl.Client and uses this to return a api.Infinispan implementation
func NewInfinispanForPod(ctx context.Context, podName string, i *v1.Infinispan, versionManager *version.Manager, kubernetes *kube.Kubernetes) (api.Infinispan, error) {
	curlClient, err := NewCurlClient(ctx, podName, i, kubernetes)
	if err != nil {
		return nil, fmt.Errorf("unable to create Infinispan client: %w", err)
	}
	operand, err := versionManager.WithRef(i.Spec.Version)
	if err != nil {
		return nil, fmt.Errorf("spec.Version '%s' doesn't exist", i.Spec.Version)
	}
	return client.New(operand, curlClient), nil
}

// NewCurlClient return a new curl.Client using the admin credentials associated with the v1.Infinispan instance
func NewCurlClient(ctx context.Context, podName string, i *v1.Infinispan, kubernetes *kube.Kubernetes) (*curl.Client, error) {
	pass, err := users.AdminPassword(i.GetOperatorUser(), i.GetAdminSecretName(), i.Namespace, kubernetes, ctx)
	if err != nil {
		return nil, fmt.Errorf("unable to retrieve operator admin identities when creating Curl client: %w", err)
	}
	curlClient := curl.New(curl.Config{
		Credentials: &curl.Credentials{
			Username: i.GetOperatorUser(),
			Password: pass,
		},
		Container: InfinispanContainer,
		Podname:   podName,
		Namespace: i.Namespace,
		Protocol:  "http",
		Port:      consts.InfinispanAdminPort,
	}, kubernetes)
	return curlClient, nil
}
