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
)

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

type CacheManagerInfo struct {
	Coordinator bool           `json:"coordinator"`
	SitesView   *[]interface{} `json:"sites_view,omitempty"`
}

type Logger struct {
	Name  string `json:"name"`
	Level string `json:"level"`
}

func (i CacheManagerInfo) GetSitesView() (map[string]bool, error) {
	sitesView := make(map[string]bool)
	if i.SitesView == nil {
		return nil, fmt.Errorf("retrieving the cross-site view is not supported with the server image you are using")
	}
	for _, site := range *i.SitesView {
		sitesView[site.(string)] = true
	}
	return sitesView, nil
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
	GetCacheManagerInfo(cacheManagerName, podName string) (*CacheManagerInfo, error)
	GetLoggers(podName string) (map[string]string, error)
	SetLogger(podName, loggerName, loggerLevel string) error
}

// NewClusterNoAuth creates a new instance of Cluster without authentication
func NewClusterNoAuth(namespace string, protocol string, kubernetes *kube.Kubernetes) *Cluster {
	return cluster(namespace, protocol, nil, kubernetes)
}

// NewCluster creates a new instance of Cluster
func NewCluster(username, password, namespace string, protocol string, kubernetes *kube.Kubernetes) *Cluster {
	credentials := &ispnclient.Credentials{
		Username: username,
		Password: password,
	}
	return cluster(namespace, protocol, credentials, kubernetes)
}

func cluster(namespace, protocol string, credentials *ispnclient.Credentials, kubernetes *kube.Kubernetes) *Cluster {
	client := curl.New(ispnclient.HttpConfig{
		Credentials: credentials,
		Namespace:   namespace,
		Protocol:    protocol,
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
	rsp, err, reason := c.Client.Post(podName, consts.ServerHTTPClusterStop, "", nil)
	return validateResponse(rsp, reason, err, "during graceful shutdown", http.StatusNoContent)
}

// GetClusterMembers get the cluster members as seen by a given pod
func (c Cluster) GetClusterMembers(podName string) (members []string, err error) {
	rsp, err, reason := c.Client.Get(podName, consts.ServerHTTPHealthPath, nil)
	if err = validateResponse(rsp, reason, err, "getting cluster members", http.StatusOK); err != nil {
		return
	}

	defer func() {
		cerr := rsp.Body.Close()
		if err == nil {
			err = cerr
		}
	}()

	var health Health
	if err := json.NewDecoder(rsp.Body).Decode(&health); err != nil {
		return nil, fmt.Errorf("unable to decode: %w", err)
	}
	return health.ClusterHealth.Nodes, nil
}

// ExistsCache returns true if cacheName cache exists on the podName pod
func (c Cluster) ExistsCache(cacheName, podName string) (bool, error) {
	path := fmt.Sprintf("%s/caches/%s", consts.ServerHTTPBasePath, cacheName)
	rsp, err, reason := c.Client.Head(podName, path, nil)
	if err := validateResponse(rsp, reason, err, "validating cache exists", http.StatusOK, http.StatusNoContent, http.StatusNotFound); err != nil {
		return false, err
	}

	switch rsp.StatusCode {
	case http.StatusOK, http.StatusNoContent:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	}
	return false, nil
}

// CacheNames return the names of the cluster caches available on the pod `podName`
func (c Cluster) CacheNames(podName string) (caches []string, err error) {
	path := fmt.Sprintf("%s/caches", consts.ServerHTTPBasePath)
	rsp, err, reason := c.Client.Get(podName, path, nil)
	if err = validateResponse(rsp, reason, err, "getting caches", http.StatusOK); err != nil {
		return
	}

	defer func() {
		cerr := rsp.Body.Close()
		if err == nil {
			err = cerr
		}
	}()

	if err := json.NewDecoder(rsp.Body).Decode(&caches); err != nil {
		return nil, fmt.Errorf("unable to decode: %w", err)
	}
	return
}

// CreateCacheWithTemplate create cluster cache on the pod `podName`
func (c Cluster) CreateCacheWithTemplate(cacheName, cacheXML, podName string) error {
	headers := make(map[string]string)
	headers["Content-Type"] = "application/xml"

	path := fmt.Sprintf("%s/caches/%s", consts.ServerHTTPBasePath, cacheName)
	rsp, err, reason := c.Client.Post(podName, path, cacheXML, headers)
	return validateResponse(rsp, reason, err, "creating cache", http.StatusOK)
}

// CreateCacheWithTemplateName create cluster cache on the pod `podName`
func (c Cluster) CreateCacheWithTemplateName(cacheName, templateName, podName string) error {
	path := fmt.Sprintf("%s/caches/%s?template=%s", consts.ServerHTTPBasePath, cacheName, templateName)
	rsp, err, reason := c.Client.Post(podName, path, "", nil)
	return validateResponse(rsp, reason, err, "creating cache with template", http.StatusOK)
}

func (c Cluster) GetMemoryLimitBytes(podName string) (uint64, error) {
	command := []string{"cat", "/sys/fs/cgroup/memory/memory.limit_in_bytes"}
	execOptions := kube.ExecOptions{Command: command, PodName: podName, Namespace: c.Namespace}
	execOut, execErr, err := c.Kubernetes.ExecWithOptions(execOptions)

	if err != nil {
		return 0, fmt.Errorf("unexpected error getting memory limit bytes, stderr: %v, err: %w", execErr, err)
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
		return 0, fmt.Errorf("unexpected error getting max unbounded memory, stderr: %v, err: %w", execErr, err)
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
func (c Cluster) GetMetrics(podName, postfix string) (buf *bytes.Buffer, err error) {
	headers := make(map[string]string)
	headers["Accept"] = "application/json"

	path := fmt.Sprintf("metrics/%s", postfix)
	rsp, err, reason := c.Client.Get(podName, path, headers)
	if err = validateResponse(rsp, reason, err, "getting metrics", http.StatusOK); err != nil {
		return
	}

	defer func() {
		cerr := rsp.Body.Close()
		if err == nil {
			err = cerr
		}
	}()

	buf = new(bytes.Buffer)
	if _, err = buf.ReadFrom(rsp.Body); err != nil {
		return
	}
	return
}

// GetCacheManagerInfo via REST v2 interface
func (c Cluster) GetCacheManagerInfo(cacheManagerName, podName string) (info *CacheManagerInfo, err error) {
	path := fmt.Sprintf("%s/cache-managers/%s", consts.ServerHTTPBasePath, cacheManagerName)
	rsp, err, reason := c.Client.Get(podName, path, nil)
	if err = validateResponse(rsp, reason, err, "getting cache manager info", http.StatusOK); err != nil {
		return
	}

	defer func() {
		cerr := rsp.Body.Close()
		if err == nil {
			err = cerr
		}
	}()

	if err = json.NewDecoder(rsp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("unable to decode: %w", err)
	}
	return
}

func (c Cluster) GetLoggers(podName string) (lm map[string]string, err error) {
	rsp, err, reason := c.Client.Get(podName, consts.ServerHTTPLoggersPath, nil)
	if err = validateResponse(rsp, reason, err, "getting cluster loggers", http.StatusOK); err != nil {
		return
	}

	defer func() {
		cerr := rsp.Body.Close()
		if err == nil {
			err = cerr
		}
	}()

	var loggers []Logger
	if err := json.NewDecoder(rsp.Body).Decode(&loggers); err != nil {
		return nil, fmt.Errorf("unable to decode: %w", err)
	}
	lm = make(map[string]string)
	for _, logger := range loggers {
		if logger.Name != "" {
			lm[logger.Name] = logger.Level
		}
	}
	return
}

func (c Cluster) SetLogger(podName, loggerName, loggerLevel string) error {
	path := fmt.Sprintf(consts.ServerHTTPModifyLoggerPath, loggerName, strings.ToUpper(loggerLevel))
	rsp, err, reason := c.Client.Put(podName, path, "", nil)
	if err := validateResponse(rsp, reason, err, "setting cluster logger", http.StatusNoContent); err != nil {
		return err
	}
	return nil
}

func validateResponse(rsp *http.Response, reason string, inperr error, entity string, validCodes ...int) (err error) {
	if inperr != nil {
		return fmt.Errorf("unexpected error %s, stderr: %s, err: %w", entity, reason, inperr)
	}

	if rsp == nil || len(validCodes) == 0 {
		return
	}

	for _, code := range validCodes {
		if code == rsp.StatusCode {
			return
		}
	}

	defer func() {
		cerr := rsp.Body.Close()
		if err == nil {
			err = cerr
		}
	}()

	responseBody, responseErr := ioutil.ReadAll(rsp.Body)
	if responseErr != nil {
		return fmt.Errorf("server side error %s. Unable to read response body, %w", entity, responseErr)
	}
	return fmt.Errorf("unexpected error %s, response: %v", entity, consts.GetWithDefault(string(responseBody), rsp.Status))
}
