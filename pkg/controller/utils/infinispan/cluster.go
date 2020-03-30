package infinispan

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/infinispan/infinispan-operator/pkg/controller/constants"
	"github.com/infinispan/infinispan-operator/pkg/controller/utils/k8s"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.Log.WithName("cluster_util")

// GetClusterSize returns the size of the cluster as seen by a given pod
func (c Cluster) GetClusterSize(secretName, podName, namespace, protocol string) (int, error) {
	members, err := c.GetClusterMembers(secretName, podName, namespace, protocol)
	if err != nil {
		return -1, err
	}

	return len(members), nil
}

// GracefulShutdown performs clean cluster shutdown
func (c Cluster) GracefulShutdown(secretName, podName, namespace, protocol string) error {
	podIP, err := c.Kubernetes.GetPodIP(podName, namespace)
	if err != nil {
		return err
	}
	pass, err := c.Kubernetes.GetPassword("operator", secretName, namespace)
	if err != nil {
		return err
	}
	httpURL := fmt.Sprintf("%s://%v:11222/%s", protocol, podIP, constants.ServerHTTPClusterStop)
	commands := []string{"curl", "-X", "GET", "--insecure", "-u", fmt.Sprintf("operator:%v", pass), httpURL}

	logger := log.WithValues("Request.Namespace", namespace, "Secret.Name", secretName, "Pod.Name", podName)
	logger.Info("get cluster members", "url", httpURL)

	execOptions := k8s.ExecOptions{Command: commands, PodName: podName, Namespace: namespace}
	_, execErr, err := c.Kubernetes.ExecWithOptions(execOptions)
	if err == nil {
		return nil
	}
	return fmt.Errorf("unexpected error getting cluster members, stderr: %v, err: %v", execErr, err)
}

// GetClusterMembers get the cluster members as seen by a given pod
func (c Cluster) GetClusterMembers(secretName, podName, namespace, protocol string) ([]string, error) {
	podIP, err := c.Kubernetes.GetPodIP(podName, namespace)
	if err != nil {
		return nil, err
	}

	pass, err := c.Kubernetes.GetPassword("operator", secretName, namespace)
	if err != nil {
		return nil, err
	}

	httpURL := fmt.Sprintf("%s://%v:11222/%s", protocol, podIP, constants.ServerHTTPHealthPath)
	commands := []string{"curl", "--insecure", "-u", fmt.Sprintf("operator:%v", pass), httpURL}

	logger := log.WithValues("Request.Namespace", namespace, "Secret.Name", secretName, "Pod.Name", podName)
	logger.Info("get cluster members", "url", httpURL)

	execOptions := k8s.ExecOptions{Command: commands, PodName: podName, Namespace: namespace}
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

func (c Cluster) ExistsCache(cacheName, secretName, podName, namespace, protocol string) bool {
	podIP, err := c.Kubernetes.GetPodIP(podName, namespace)
	if err != nil {
		return false
	}

	pass, err := c.Kubernetes.GetPassword("operator", secretName, namespace)
	if err != nil {
		return false
	}

	httpURL := fmt.Sprintf("%s://%s:11222/rest/v2/caches/%s?action=config", protocol, podIP, cacheName)
	commands := []string{"curl",
		"--insecure",
		"-o", "/dev/null", "-w", "%{http_code}", // ignore output and get http status code
		"-u", fmt.Sprintf("operator:%v", pass),
		"--head",
		httpURL,
	}

	execOptions := k8s.ExecOptions{Command: commands, PodName: podName, Namespace: namespace}
	execOut, _, err := c.Kubernetes.ExecWithOptions(execOptions)
	if err != nil {
		return false
	}

	httpCode, err := strconv.ParseUint(execOut.String(), 10, 64)
	if err != nil {
		return false
	}

	switch httpCode {
	case 200:
		return true
	default:
		return false
	}
}

func (c Cluster) CreateCache(cacheName, cacheXml, secretName, podName, namespace, protocol string) error {
	podIP, err := c.Kubernetes.GetPodIP(podName, namespace)
	if err != nil {
		return err
	}

	pass, err := c.Kubernetes.GetPassword("operator", secretName, namespace)
	if err != nil {
		return err
	}

	httpURL := fmt.Sprintf("%s://%s:11222/rest/v2/caches/%s", protocol, podIP, cacheName)
	commands := []string{"curl",
		"--insecure",
		"-w", "\n%{http_code}", // add http status at the end
		"-d", fmt.Sprintf("%s", cacheXml),
		"-H", "Content-Type: application/xml",
		"-u", fmt.Sprintf("operator:%v", pass),
		"-X", "POST",
		httpURL,
	}

	execOptions := k8s.ExecOptions{Command: commands, PodName: podName, Namespace: namespace}
	execOut, execErr, err := c.Kubernetes.ExecWithOptions(execOptions)
	if err != nil {
		return fmt.Errorf("unexpected error creating cache, stderr: %v, err: %v", execErr, err)
	}

	// Split lines in standard output, HTTP status code will be last
	execOutLines := strings.Split(execOut.String(), "\n")

	httpCode, err := strconv.ParseUint(execOutLines[len(execOutLines)-1], 10, 64)
	if err != nil {
		return err
	}

	if httpCode > 299 || httpCode < 200 {
		return fmt.Errorf("server side error creating cache: %s", execOut.String())
	}

	logger := log.WithValues("Request.Namespace", namespace, "Secret.Name", secretName, "Pod.Name", podName)
	logger.Info("create cache completed successfully", "http code", httpCode)
	return nil
}

func (c Cluster) GetMemoryLimitBytes(podName, namespace string) (uint64, error) {
	command := []string{"cat", "/sys/fs/cgroup/memory/memory.limit_in_bytes"}
	execOptions := k8s.ExecOptions{Command: command, PodName: podName, Namespace: namespace}
	execOut, execErr, err := c.Kubernetes.ExecWithOptions(execOptions)

	if err != nil {
		return 0, fmt.Errorf("unexpected error getting memory limit bytes, stderr: %v, err: %v", execErr, err)
	}

	result := strings.TrimSuffix(execOut.String(), "\n")
	limitBytes, err := strconv.ParseUint(result, 10, 64)
	if err != nil {
		return 0, err
	}

	return limitBytes, nil
}

func (c Cluster) GetMaxMemoryUnboundedBytes(podName, namespace string) (uint64, error) {
	command := []string{"cat", "/proc/meminfo"}
	execOptions := k8s.ExecOptions{Command: command, PodName: podName, Namespace: namespace}
	execOut, execErr, err := c.Kubernetes.ExecWithOptions(execOptions)

	if err != nil {
		return 0, fmt.Errorf("unexpected error getting max unbounded memory, stderr: %v, err: %v", execErr, err)
	}

	result := execOut.String()
	lines := strings.Split(result, "\n")
	for _, line := range lines {
		if strings.Contains(line, "MemTotal:") {
			tokens := strings.Fields(line)
			maxUnboundKb, err := strconv.ParseUint(tokens[1], 10, 64)
			if err != nil {
				return 0, err
			}
			return maxUnboundKb * 1024, nil
		}
	}

	return 0, fmt.Errorf("meminfo lacking MemTotal information")
}
