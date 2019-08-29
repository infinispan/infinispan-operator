package util

import (
	"encoding/json"
	"fmt"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.Log.WithName("cluster_util")

// Cluster abstracts interaction with an Infinispan cluster
type Cluster struct {
	Kubernetes *Kubernetes
}

// NewCluster creates a new instance of Cluster
func NewCluster(kubernetes *Kubernetes) *Cluster {
	return &Cluster{Kubernetes: kubernetes}
}

// ClusterInterface represents the interface of a Cluster instance
type ClusterInterface interface {
	GetClusterMembers(secretName, podName, namespace string) ([]string, error)
}

// GetPassword returns password associated with a user in a given secret
func (c Cluster) GetPassword(user, secretName, namespace string) (string, error) {
	secret, err := c.Kubernetes.GetSecret(secretName, namespace)
	if err != nil {
		return "", nil
	}

	descriptor := secret.Data["identities.yaml"]
	pass, err := FindPassword(user, descriptor)
	if err != nil {
		return "", err
	}

	return pass, nil
}

// GetClusterSize returns the size of the cluster as seen by a given pod
func (c Cluster) GetClusterSize(secretName, podName, namespace string) (int, error) {
	members, err := c.GetClusterMembers(secretName, podName, namespace)
	if err != nil {
		return -1, err
	}

	return len(members), nil
}

// ClusterHealth represents the health of the cluster
type ClusterHealth struct {
	Nodes []string `json:"node_names"`
}

// Health represents the health of an Infinispan server
type Health struct {
	ClusterHealth ClusterHealth `json:"cluster_health"`
}

// GetClusterMembers get the cluster members as seen by a given pod
func (c Cluster) GetClusterMembers(secretName, podName, namespace string) ([]string, error) {
	podIP, err := c.Kubernetes.GetPodIP(podName, namespace)
	if err != nil {
		return nil, err
	}

	pass, err := c.GetPassword("operator", secretName, namespace)
	if err != nil {
		return nil, err
	}

	httpURL := fmt.Sprintf("http://%v:11222/rest/v2/cache-managers/DefaultCacheManager/health", podIP)
	commands := []string{"curl", "-u", fmt.Sprintf("operator:%v", pass), httpURL}

	logger := log.WithValues("Request.Namespace", namespace, "Secret.Name", secretName, "Pod.Name", podName)
	logger.Info("get cluster members", "url", httpURL)

	execOptions := ExecOptions{Command: commands, PodName: podName, Namespace: namespace}
	execOut, execErr, err := c.Kubernetes.ExecWithOptions(execOptions)
	if err == nil {
		result := execOut.Bytes()

		var health Health
		err = json.Unmarshal(result, &health)
		if err != nil {
			return nil, fmt.Errorf("unable to decode: %v", err)
		}

		return health.ClusterHealth.Nodes, nil
	}
	return nil, fmt.Errorf("unexpected error getting cluster members, stderr: %v, err: %v", execErr, err)
}
