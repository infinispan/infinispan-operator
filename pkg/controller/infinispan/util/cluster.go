package util

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
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
	GetClusterSize(user, pass, podName, namespace, protocol string) (int, error)
	GracefulShutdown(user, pass, podName, namespace, protocol string) error
	GetClusterMembers(user, pass, podName, namespace, protocol string) ([]string, error)
	ExistsCache(user, pass, cacheName, podName, namespace, protocol string) (bool, error)
	CreateCacheWithTemplate(user, pass, cacheName, cacheXML, podName, namespace, protocol string) error
	CreateCacheWithTemplateName(user, pass, cacheName, templateName, podName, namespace, protocol string) error
	GetMemoryLimitBytes(podName, namespace string) (uint64, error)
	GetMaxMemoryUnboundedBytes(podName, namespace string) (uint64, error)
	CacheNames(user, pass, podName, namespace, protocol string) ([]string, error)
	GetPassword(user, secretName, namespace string) (string, error)
}

// GetClusterSize returns the size of the cluster as seen by a given pod
func (c Cluster) GetClusterSize(user, pass, podName, namespace, protocol string) (int, error) {
	members, err := c.GetClusterMembers(user, pass, podName, namespace, protocol)
	if err != nil {
		return -1, err
	}

	return len(members), nil
}

// GracefulShutdown performs clean cluster shutdown
func (c Cluster) GracefulShutdown(user, pass, podName, namespace, protocol string) error {
	podIP, err := c.Kubernetes.GetPodIP(podName, namespace)
	if err != nil {
		return err
	}
	httpURL := fmt.Sprintf("%s://%v:%d/%s", protocol, podIP, consts.ClusterHotRodPort, consts.ServerHTTPClusterStop)
	commands := []string{"curl", "-X", "GET", "--insecure", "-u", fmt.Sprintf("%v:%v", user, pass), httpURL}

	logger := log.WithValues("Request.Namespace", namespace, "Pod.Name", podName)
	logger.Info("get cluster members", "url", httpURL)

	execOptions := ExecOptions{Command: commands, PodName: podName, Namespace: namespace}
	_, execErr, err := c.Kubernetes.ExecWithOptions(execOptions)
	if err == nil {
		return nil
	}
	return fmt.Errorf("unexpected error getting cluster members, stderr: %v, err: %v", execErr, err)
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
func (c Cluster) GetClusterMembers(user, pass, podName, namespace, protocol string) ([]string, error) {
	podIP, err := c.Kubernetes.GetPodIP(podName, namespace)
	if err != nil {
		return nil, err
	}

	httpURL := fmt.Sprintf("%s://%v:%d/%s", protocol, podIP, consts.ClusterHotRodPort, consts.ServerHTTPHealthPath)
	commands := []string{"curl", "--insecure", "-u", fmt.Sprintf("%v:%v", user, pass), httpURL}

	logger := log.WithValues("Request.Namespace", namespace, "Pod.Name", podName)
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

// ExistsCache returns true if cacheName cache exists on the podName pod
func (c Cluster) ExistsCache(user, pass, cacheName, podName, namespace, protocol string) (bool, error) {
	podIP, err := c.Kubernetes.GetPodIP(podName, namespace)
	if err != nil {
		return false, err
	}

	httpURL := fmt.Sprintf("%s://%s:%d/rest/v2/caches/%s?action=config", protocol, podIP, consts.ClusterHotRodPort, cacheName)
	commands := []string{"curl",
		"--insecure",
		"--http1.1",
		"-o", "/dev/null", "-w", "%{http_code}", // ignore output and get http status code
		"-u", fmt.Sprintf("%v:%v", user, pass),
		"--head",
		httpURL,
	}

	execOptions := ExecOptions{Command: commands, PodName: podName, Namespace: namespace}
	execOut, _, err := c.Kubernetes.ExecWithOptions(execOptions)
	if err != nil {
		return false, err
	}

	httpCode, err := strconv.ParseUint(execOut.String(), 10, 64)
	if err != nil {
		return false, err
	}

	switch httpCode {
	case 200:
		return true, nil
	default:
		return false, nil
	}
}

// CacheNames return the names of the cluster caches available on the pod `podName`
func (c Cluster) CacheNames(user, pass, podName, namespace, protocol string) ([]string, error) {
	podIP, err := c.Kubernetes.GetPodIP(podName, namespace)
	if err != nil {
		return nil, err
	}
	httpURL := fmt.Sprintf("%s://%s:%d/rest/v2/caches", protocol, podIP, consts.ClusterHotRodPort)
	commands := []string{"curl", "--insecure", "-u", fmt.Sprintf("%v:%v", user, pass), httpURL}
	logger := log.WithValues("Request.Namespace", namespace, "Pod.Name", podName)
	logger.Info("get caches list", "url", httpURL)
	execOptions := ExecOptions{Command: commands, PodName: podName, Namespace: namespace}
	execOut, execErr, err := c.Kubernetes.ExecWithOptions(execOptions)
	var caches []string
	if err == nil {
		result := execOut.Bytes()
		err = json.Unmarshal(result, &caches)
		return caches, err
	}
	return nil, fmt.Errorf("unexpected error getting cluster members, stderr: %v, err: %v", execErr, err)
}

// CreateCacheWithTemplate create cluster cache on the pod `podName`
func (c Cluster) CreateCacheWithTemplate(user, pass, cacheName, cacheXML, podName, namespace, protocol string) error {
	podIP, err := c.Kubernetes.GetPodIP(podName, namespace)
	if err != nil {
		return err
	}

	httpURL := fmt.Sprintf("%s://%s:%d/rest/v2/caches/%s", protocol, podIP, consts.ClusterHotRodPort, cacheName)
	commands := []string{"curl",
		"--insecure",
		"--http1.1",
		"-w", "\n%{http_code}", // add http status at the end
		"-d", fmt.Sprintf("%s", cacheXML),
		"-H", "Content-Type: application/xml",
		"-u", fmt.Sprintf("%v:%v", user, pass),
		"-X", "POST",
		httpURL,
	}

	execOptions := ExecOptions{Command: commands, PodName: podName, Namespace: namespace}
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

	logger := log.WithValues("Request.Namespace", namespace, "Pod.Name", podName)
	logger.Info("create cache completed successfully", "http code", httpCode)
	return nil
}

// CreateCacheWithTemplateName create cluster cache on the pod `podName`
func (c Cluster) CreateCacheWithTemplateName(user, pass, cacheName, templateName, podName, namespace, protocol string) error {
	podIP, err := c.Kubernetes.GetPodIP(podName, namespace)
	if err != nil {
		return err
	}

	httpURL := fmt.Sprintf("%s://%s:%d/rest/v2/caches/%s?template=%s", protocol, podIP, consts.ClusterHotRodPort, cacheName, templateName)
	commands := []string{"curl",
		"--insecure",
		"--http1.1",
		"-w", "\n%{http_code}", // add http status at the end
		"-u", fmt.Sprintf("%v:%v", user, pass),
		"-X", "POST",
		httpURL,
	}

	execOptions := ExecOptions{Command: commands, PodName: podName, Namespace: namespace}
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

	logger := log.WithValues("Request.Namespace", namespace, "Pod.Name", podName)
	logger.Info("create cache completed successfully", "http code", httpCode)
	return nil
}

func (c Cluster) GetMemoryLimitBytes(podName, namespace string) (uint64, error) {
	command := []string{"cat", "/sys/fs/cgroup/memory/memory.limit_in_bytes"}
	execOptions := ExecOptions{Command: command, PodName: podName, Namespace: namespace}
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
	execOptions := ExecOptions{Command: command, PodName: podName, Namespace: namespace}
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

// GetPassword returns the user's password
func (c Cluster) GetPassword(user, secretName, namespace string) (string, error) {
	return c.Kubernetes.GetPassword(user, secretName, namespace)
}

// Return handler for querying cluster status
func ClusterStatusHandler(scheme corev1.URIScheme) corev1.Handler {
	return corev1.Handler{
		HTTPGet: &corev1.HTTPGetAction{
			Scheme: scheme,
			Path:   consts.ServerHTTPHealthStatusPath,
			Port:   intstr.FromInt(consts.ClusterHotRodPort)},
	}
}
