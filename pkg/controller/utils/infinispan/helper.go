package infinispan

import (
	"github.com/infinispan/infinispan-operator/pkg/controller/constants"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// ClusterHealth represents the health of the cluster
type ClusterHealth struct {
	Nodes []string `json:"node_names"`
}

// Health represents the health of an Infinispan server
type Health struct {
	ClusterHealth ClusterHealth `json:"cluster_health"`
}

// Return handler for querying cluster status
func ClusterStatusHandler(scheme corev1.URIScheme) corev1.Handler {
	return corev1.Handler{
		HTTPGet: &corev1.HTTPGetAction{
			Scheme: scheme,
			Path:   constants.ServerHTTPHealthStatusPath,
			Port:   intstr.FromInt(11222)},
	}
}
