package infinispan

import (
	cluster2 "github.com/infinispan/infinispan-operator/pkg/controller/utils/infinispan"
	"github.com/infinispan/infinispan-operator/pkg/controller/utils/k8s"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewFakeReconciler creates a new fake Reconciler for unit testing
func NewFakeReconciler(client client.Client, scheme *runtime.Scheme, fakeKubernetes *k8s.Kubernetes, fakeCluster cluster2.ClusterInterface) ReconcileInfinispan {
	kubernetes = fakeKubernetes
	cluster = fakeCluster
	return ReconcileInfinispan{client, scheme}
}

// GetClient returns Kubernetes client for unit testing
func (r *ReconcileInfinispan) GetClient() client.Client {
	return r.client
}
