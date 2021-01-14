package infinispan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"

	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	ispnclient "github.com/infinispan/infinispan-operator/pkg/infinispan/client/http"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/http/curl"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.Log.WithName("cluster_util")

// Cluster abstracts interaction with an Infinispan cluster
type Cluster struct {
	Kubernetes *kube.Kubernetes
	Client     ispnclient.HttpClient
	Namespace  string
}

// ClusterHealth represents the health of the cluster
type ClusterHealth struct {
	Nodes []string `json:"node_names"`
}

// Health represents the health of an Infinispan server
type Health struct {
	ClusterHealth ClusterHealth `json:"cluster_health"`
}

// ClusterInterface represents the interface of a Cluster instance
type ClusterInterface interface {
	GetClusterSize(podName string) (int, error)
	GracefulShutdown(podName string) error
	GetClusterMembers(podName string) ([]string, error)
	ExistsCache(cacheName, podName string) (bool, error)
	CreateCacheWithTemplate(cacheName, cacheXML, podName string) error
	CreateCacheWithTemplateName(cacheName, templateName, podName string) error
	GetMemoryLimitBytes(podName string) (uint64, error)
	GetMaxMemoryUnboundedBytes(podName string) (uint64, error)
	CacheNames(podName string) ([]string, error)
	GetMetrics(podName, postfix string) (*bytes.Buffer, error)
	GetCacheManagerInfo(cacheManagerName, podName string) (map[string]interface{}, error)
}

// NewCluster creates a new instance of Cluster
func NewCluster(username, password, namespace string, protocol string, kubernetes *kube.Kubernetes) *Cluster {
	client := curl.New(ispnclient.HttpConfig{
		Username:  username,
		Password:  password,
		Namespace: namespace,
		Protocol:  protocol,
	}, kubernetes)

	return &Cluster{
		Kubernetes: kubernetes,
		Client:     client,
		Namespace:  namespace,
	}
}

// GetClusterSize returns the size of the cluster as seen by a given pod
func (c Cluster) GetClusterSize(podName string) (int, error) {
	members, err := c.GetClusterMembers(podName)
	if err != nil {
		return -1, err
	}

	return len(members), nil
}

// GracefulShutdown performs clean cluster shutdown
func (c Cluster) GracefulShutdown(podName string) error {
	_, err, reason := c.Client.Post(podName, consts.ServerHTTPClusterStop, "", nil)
	if err != nil {
		return fmt.Errorf("unexpected error during graceful shutdown, stderr: %v, err: %v", reason, err)
	}
	return nil
}

// GetClusterMembers get the cluster members as seen by a given pod
func (c Cluster) GetClusterMembers(podName string) ([]string, error) {
	rsp, err, reason := c.Client.Get(podName, consts.ServerHTTPHealthPath, nil)
	if err != nil {
		return nil, fmt.Errorf("unexpected error getting cluster members, stderr: %v, err: %v", reason, err)
	}
	if rsp.StatusCode != http.StatusOK {
		defer rsp.Body.Close()
		responseBody, err := ioutil.ReadAll(rsp.Body)
		if err != nil {
			return nil, fmt.Errorf( "server side error getting cluster members. Unable to read response body, %v", err)
		}
		return nil, fmt.Errorf("unexpected error getting cluster members, response: %v", consts.GetWithDefault(string(responseBody), rsp.Status))
	}

	defer rsp.Body.Close()
	var health Health
	if err := json.NewDecoder(rsp.Body).Decode(&health); err != nil {
		return nil, fmt.Errorf("unable to decode: %v", err)
	}
	return health.ClusterHealth.Nodes, nil
}

// ExistsCache returns true if cacheName cache exists on the podName pod
func (c Cluster) ExistsCache(cacheName, podName string) (bool, error) {
	path := fmt.Sprintf("%s/caches/%s", consts.ServerHTTPBasePath, cacheName)
	rsp, err, _ := c.Client.Head(podName, path, nil)
	if err != nil {
		return false, err
	}

	switch rsp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, fmt.Errorf("HTTP response code: %d", rsp.StatusCode)
	}
}

// CacheNames return the names of the cluster caches available on the pod `podName`
func (c Cluster) CacheNames(podName string) ([]string, error) {
	path := fmt.Sprintf("%s/caches", consts.ServerHTTPBasePath)
	rsp, err, reason := c.Client.Get(podName, path, nil)
	if err != nil {
		return nil, fmt.Errorf("unexpected error getting cluster members, stderr: %v, err: %v", reason, err)
	}

	defer rsp.Body.Close()
	var caches []string
	if err := json.NewDecoder(rsp.Body).Decode(&caches); err != nil {
		return nil, fmt.Errorf("unable to decode: %v", err)
	}
	return caches, nil
}

// CreateCacheWithTemplate create cluster cache on the pod `podName`
func (c Cluster) CreateCacheWithTemplate(cacheName, cacheXML, podName string) error {
	headers := make(map[string]string)
	headers["Content-Type"] = "application/xml"

	path := fmt.Sprintf("%s/caches/%s", consts.ServerHTTPBasePath, cacheName)
	rsp, err, reason := c.Client.Post(podName, path, cacheXML, headers)
	if err != nil {
		return fmt.Errorf("unexpected error creating cache, stderr: %v, err: %v", reason, err)
	}

	defer rsp.Body.Close()
	httpCode := rsp.StatusCode
	if httpCode >= http.StatusMultipleChoices || httpCode < http.StatusOK {
		bodyBytes, err := ioutil.ReadAll(rsp.Body)
		if err != nil {
			return fmt.Errorf("server side error creating cache. Unable to read response body")
		}
		return fmt.Errorf("server side error creating cache: %s", string(bodyBytes))
	}
	return nil
}

// CreateCacheWithTemplateName create cluster cache on the pod `podName`
func (c Cluster) CreateCacheWithTemplateName(cacheName, templateName, podName string) error {
	path := fmt.Sprintf("%s/caches/%s?template=%s", consts.ServerHTTPBasePath, cacheName, templateName)
	rsp, err, reason := c.Client.Post(podName, path, "", nil)
	if err != nil {
		return fmt.Errorf("unexpected error creating cache, stderr: %v, err: %v", reason, err)
	}

	defer rsp.Body.Close()
	httpCode := rsp.StatusCode
	if httpCode >= http.StatusMultipleChoices || httpCode < http.StatusOK {
		bodyBytes, err := ioutil.ReadAll(rsp.Body)
		if err != nil {
			return fmt.Errorf("server side error creating cache. Unable to read response body")
		}
		return fmt.Errorf("server side error creating cache: %s", string(bodyBytes))
	}
	return nil
}

func (c Cluster) GetMemoryLimitBytes(podName string) (uint64, error) {
	command := []string{"cat", "/sys/fs/cgroup/memory/memory.limit_in_bytes"}
	execOptions := kube.ExecOptions{Command: command, PodName: podName, Namespace: c.Namespace}
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

func (c Cluster) GetMaxMemoryUnboundedBytes(podName string) (uint64, error) {
	command := []string{"cat", "/proc/meminfo"}
	execOptions := kube.ExecOptions{Command: command, PodName: podName, Namespace: c.Namespace}
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

// GetMetrics return pod metrics
func (c Cluster) GetMetrics(podName, postfix string) (*bytes.Buffer, error) {
	headers := make(map[string]string)
	headers["Accept"] = "application/json"

	path := fmt.Sprintf("%s/metrics/%s", consts.ServerHTTPBasePath, postfix)
	rsp, err, reason := c.Client.Get(podName, path, headers)
	if err != nil {
		return nil, fmt.Errorf("unexpected error getting metrics, stderr: %v, err: %v", reason, err)
	}
	defer rsp.Body.Close()
	buf := new(bytes.Buffer)
	buf.ReadFrom(rsp.Body)
	return buf, nil
}

// Return handler for querying cluster status
func ClusterStatusHandler(scheme corev1.URIScheme) corev1.Handler {
	return corev1.Handler{
		HTTPGet: &corev1.HTTPGetAction{
			Scheme: scheme,
			Path:   consts.ServerHTTPHealthStatusPath,
			Port:   intstr.FromInt(consts.InfinispanPort)},
	}
}

// GetCacheManagerInfo via REST v2 interface
func (c Cluster) GetCacheManagerInfo(cacheManagerName, podName string) (map[string]interface{}, error) {
	path := fmt.Sprintf("%s/cache-managers/%s", consts.ServerHTTPBasePath, cacheManagerName)
	rsp, err, reason := c.Client.Get(podName, path, nil)
	if err != nil {
		return nil, fmt.Errorf("unexpected error getting cache manager info, stderr: %v, err: %v", reason, err)
	}

	defer rsp.Body.Close()
	var info map[string]interface{}
	if err := json.NewDecoder(rsp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("unable to decode: %v", err)
	}
	return info, nil
}
