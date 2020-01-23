package util

import (
	"encoding/json"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"strconv"
	"strings"
)

var log = logf.Log.WithName("cluster_util")

const ServerHTTPBasePath = "rest/v2"
const ServerHTTPClusterStop = ServerHTTPBasePath + "/cluster?action=stop"
const ServerHTTPHealthPath = ServerHTTPBasePath + "/cache-managers/default/health"
const ServerHTTPHealthStatusPath = ServerHTTPHealthPath + "/status"

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
	GetClusterMembers(secretName, podName, namespace, protocol string) ([]string, error)
	GracefulShutdown(secretName, podName, namespace, protocol string) error
	GetMinNumberOfNodes(secretName, podName, namespace, protocol string) (int, error)
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
	pass, err := c.GetPassword("operator", secretName, namespace)
	if err != nil {
		return err
	}
	httpURL := fmt.Sprintf("%s://%v:11222/%s", protocol, podIP, ServerHTTPClusterStop)
	commands := []string{"curl", "-X", "GET", "--insecure", "-u", fmt.Sprintf("operator:%v", pass), httpURL}

	logger := log.WithValues("Request.Namespace", namespace, "Secret.Name", secretName, "Pod.Name", podName)
	logger.Info("Calling cluster stop action", "url", httpURL)

	execOptions := ExecOptions{Command: commands, PodName: podName, Namespace: namespace}
	_, execErr, err := c.Kubernetes.ExecWithOptions(execOptions)
	if err == nil {
		return nil
	}
	return fmt.Errorf("unexpected error calling cluster stop action, stderr: %v, err: %v", execErr, err)
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
func (c Cluster) GetClusterMembers(secretName, podName, namespace, protocol string) ([]string, error) {
	podIP, err := c.Kubernetes.GetPodIP(podName, namespace)
	if err != nil {
		return nil, err
	}

	pass, err := c.GetPassword("operator", secretName, namespace)
	if err != nil {
		return nil, err
	}

	httpURL := fmt.Sprintf("%s://%v:11222/%s", protocol, podIP, ServerHTTPHealthPath)
	commands := []string{"curl", "--insecure", "-u", fmt.Sprintf("operator:%v", pass), httpURL}

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

func (c Cluster) ExistsCache(cacheName, secretName, podName, namespace, protocol string) bool {
	podIP, err := c.Kubernetes.GetPodIP(podName, namespace)
	if err != nil {
		return false
	}

	pass, err := c.GetPassword("operator", secretName, namespace)
	if err != nil {
		return false
	}

	httpURL := fmt.Sprintf("%s://%s:11222/rest/v2/caches/%s?action=config", protocol, podIP, cacheName)
	commands := []string{"curl",
		"-o", "/dev/null", "-w", "%{http_code}", // ignore output and get http status code
		"-u", fmt.Sprintf("operator:%v", pass),
		"--head",
		httpURL,
	}

	execOptions := ExecOptions{Command: commands, PodName: podName, Namespace: namespace}
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

	pass, err := c.GetPassword("operator", secretName, namespace)
	if err != nil {
		return err
	}

	httpURL := fmt.Sprintf("%s://%s:11222/rest/v2/caches/%s", protocol, podIP, cacheName)
	commands := []string{"curl",
		"-w", "\n%{http_code}", // add http status at the end
		"-d", fmt.Sprintf("%s", cacheXml),
		"-H", "Content-Type: application/xml",
		"-u", fmt.Sprintf("operator:%v", pass),
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

	logger := log.WithValues("Request.Namespace", namespace, "Secret.Name", secretName, "Pod.Name", podName)
	logger.Info("create cache completed successfully", "http code", httpCode)
	return nil
}

// Return handler for querying cluster status
func ClusterStatusHandler(scheme corev1.URIScheme) corev1.Handler {
	return corev1.Handler{
		HTTPGet: &corev1.HTTPGetAction{
			Scheme: scheme,
			Path:   ServerHTTPHealthStatusPath,
			Port:   intstr.FromInt(11222)},
	}
}

// GetMinNumberOfNodes get the minimum number of nodes valued from a pod
func (c Cluster) GetMinNumberOfNodes(secretName, podName, namespace, protocol string) (int, error) {
	podIP, err := c.Kubernetes.GetPodIP(podName, namespace)
	if err != nil {
		return 0, err
	}

	pass, err := c.GetPassword("operator", secretName, namespace)
	if err != nil {
		return 0, err
	}

	httpURL := fmt.Sprintf("%s://%v:11222/%s", protocol, podIP, "metrics/application/Cache_Statistics_requiredMinimumNumberOfNodes")
	commands := []string{"curl", "--insecure", "-u", fmt.Sprintf("operator:%v", pass), "-H", "Accept: application/json", httpURL}

	logger := log.WithValues("Request.Namespace", namespace, "Secret.Name", secretName, "Pod.Name", podName)
	logger.Info("get metrics", "url", httpURL)

	execOptions := ExecOptions{Command: commands, PodName: podName, Namespace: namespace}
	execOut, execErr, err := c.Kubernetes.ExecWithOptions(execOptions)
	if err == nil {
		result := execOut.Bytes()
		var minNodesPerCaches map[string]int
		maxMin := 0
		err = json.Unmarshal(result, &minNodesPerCaches)
		s := string(result)
		fmt.Println(s)
		if err == nil {
			for _, value := range minNodesPerCaches {
				if value > maxMin {
					maxMin = value
				}
			}
			return maxMin, nil
		}
		return 0, err
	}
	return 0, fmt.Errorf("unexpected error getting cluster members, stderr: %v, err: %v", execErr, err)
}
