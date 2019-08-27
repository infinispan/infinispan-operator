package util

import (
	"encoding/json"
	"fmt"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.Log.WithName("cluster_util")

// ClusterMembers is a lambda function for getting cluster members from server
type ClusterMembers func(secretName, podName, namespace string) ([]string, error)

// GetPassword returns password associated with a user in a given secret
func GetPassword(user, secretName, namespace string) (string, error) {
	secret, err := GetSecret(secretName, namespace)
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
func GetClusterSize(secretName, podName, namespace string) (int, error) {
	members, err := GetClusterMembers(secretName, podName, namespace)
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
func GetClusterMembers(secretName, podName, namespace string) ([]string, error) {
	podIP, err := GetPodIP(podName, namespace)
	if err != nil {
		return nil, err
	}

	pass, err := GetPassword("operator", secretName, namespace)
	if err != nil {
		return nil, err
	}

	httpURL := fmt.Sprintf("http://%v:11222/rest/v2/cache-managers/DefaultCacheManager/health", podIP)
	commands := []string{"curl", "-u", fmt.Sprintf("operator:%v", pass), httpURL}

	logger := log.WithValues("Request.Namespace", namespace, "Secret.Name", secretName, "Pod.Name", podName)
	logger.Info("get cluster members", "url", httpURL)

	execOptions := ExecOptions{Command: commands, PodName: podName, Namespace: namespace}
	execOut, execErr, err := ExecWithOptions(execOptions)
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
