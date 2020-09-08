package infinispan

import (
	ispnutil "github.com/infinispan/infinispan-operator/pkg/controller/infinispan/util"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewFakeReconciler creates a new fake Reconciler for unit testing
func NewFakeReconciler(client client.Client, scheme *runtime.Scheme, fakeKubernetes *ispnutil.Kubernetes, fakeCluster ispnutil.ClusterInterface) ReconcileInfinispan {
	kubernetes = fakeKubernetes
	return ReconcileInfinispan{client, scheme}
}

// GetClient returns Kubernetes client for unit testing
func (r *ReconcileInfinispan) GetClient() client.Client {
	return r.client
}
