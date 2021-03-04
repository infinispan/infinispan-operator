package infinispan

import (
	"github.com/infinispan/infinispan-operator/pkg/controller/base"
	ispn "github.com/infinispan/infinispan-operator/pkg/infinispan"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewFakeReconciler creates a new fake Reconciler for unit testing
func NewFakeReconciler(client client.Client, fakeKubernetes *kube.Kubernetes, fakeCluster ispn.ClusterInterface) ReconcileInfinispan {
	return ReconcileInfinispan{base.NewReconcilerBaseFromManager("FakeReconciler", nil)}
}
