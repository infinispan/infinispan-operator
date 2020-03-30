package infinispan

import "github.com/infinispan/infinispan-operator/pkg/controller/utils/k8s"

// Cluster abstracts interaction with an Infinispan cluster
type Cluster struct {
	Kubernetes *k8s.Kubernetes
}

// NewCluster creates a new instance of Cluster
func NewCluster(kubernetes *k8s.Kubernetes) *Cluster {
	return &Cluster{Kubernetes: kubernetes}
}

// ClusterInterface represents the interface of a Cluster instance
type ClusterInterface interface {
	GetClusterSize(secretName, podName, namespace, protocol string) (int, error)
	GracefulShutdown(secretName, podName, namespace, protocol string) error
	GetClusterMembers(secretName, podName, namespace, protocol string) ([]string, error)
	ExistsCache(cacheName, secretName, podName, namespace, protocol string) bool
	CreateCache(cacheName, cacheXml, secretName, podName, namespace, protocol string) error
	GetMemoryLimitBytes(podName, namespace string) (uint64, error)
	GetMaxMemoryUnboundedBytes(podName, namespace string) (uint64, error)
}
