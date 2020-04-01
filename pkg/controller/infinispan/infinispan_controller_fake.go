package infinispan

import (
	"github.com/infinispan/infinispan-operator/pkg/controller/utils/k8s"
	coputil "github.com/redhat-cop/operator-utils/pkg/util"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewFakeReconciler creates a new fake Reconciler for unit testing
func NewFakeReconciler(client client.Client, scheme *runtime.Scheme, fakeKubernetes *k8s.Kubernetes, fakeCluster k8s.ClusterInterface) ReconcileInfinispan {
	k8sclient,_ := kubernetes.NewForConfig(fakeKubernetes.RestConfig)
	reconcilerBase := coputil.NewReconcilerBase(client, scheme, fakeKubernetes.RestConfig, nil)
	return ReconcileInfinispan{reconcilerBase, k8sclient, fakeCluster, fakeKubernetes}
}

// GetClient returns Kubernetes client for unit testing
func (r *ReconcileInfinispan) GetClient() client.Client {
	return r.GetClient()
}
