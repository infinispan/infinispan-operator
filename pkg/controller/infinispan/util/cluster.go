package util

import (
	"encoding/json"
	"fmt"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.Log.WithName("cluster_util")

type ClusterMembers func(secretName, podName, namespace string) ([]string, error)

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

func GetClusterSize(secretName, podName, namespace string) (int, error) {
	members, err := GetClusterMembers(secretName, podName, namespace)
	if err != nil {
		return -1, err
	}

	return len(members), nil
}

type ClusterHealth struct {
	Nodes []string `json:"node_names"`
}

type Health struct {
	ClusterHealth ClusterHealth `json:"cluster_health"`
}

// GetClusterMembers get the cluster members
func GetClusterMembers(secretName, podName, namespace string) ([]string, error) {
	podIp, err := GetPodIp(podName, namespace)
	if err != nil {
		return nil, err
	}

	pass, err := GetPassword("operator", secretName, namespace)
	if err != nil {
		return nil, err
	}

	httpUrl := fmt.Sprintf("http://%v:11222/rest/v2/cache-managers/DefaultCacheManager/health", podIp)
	commands := []string{"curl", "-u", fmt.Sprintf("operator:%v", pass), httpUrl}

	logger := log.WithValues("Request.Namespace", namespace, "Secret.Name", secretName, "Pod.Name", podName)
	logger.Info("get cluster members", "url", httpUrl)

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
