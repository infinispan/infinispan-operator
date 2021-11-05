package infinispan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"

	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	ispnclient "github.com/infinispan/infinispan-operator/pkg/infinispan/client/http"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/http/curl"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/infinispan/infinispan-operator/pkg/mime"
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
	ClusterName string         `json:"cluster_name"`
}

type Logger struct {
	Name  string `json:"name"`
	Level string `json:"level"`
}

type CacheConfiguration struct {
	Name          string          `json:"name"`
	Configuration json.RawMessage `json:"configuration"`
}

type Headers map[string]string

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
	GracefulShutdownTask(podName string) error
	GetClusterMembers(podName string) ([]string, error)
	ExistsCache(cacheName, podName string) (bool, error)
	DeleteCache(cacheName, podName string) error
	UpdateCacheWithConfiguration(cacheName, config, podName string, mimeType mime.MimeType) error
	CreateCacheWithConfiguration(cacheName, config, podName string, mimeType mime.MimeType) error
	CreateCacheWithTemplateName(cacheName, templateName, podName string) error
	CreateCache(cacheName, jsonConfig, podName string) error
	GetCacheConfig(cacheName, podName string) (cacheConfig string, err error)
	AddRemoteStore(cacheName, jsonConfig, podName string) error
	IsSourceConnected(cacheName, podName string) (bool, error)
	DisconnectSource(cacheName, podName string) error
	SyncData(cacheName, podName string) (string, error)
	ConvertCacheConfiguration(config, podName string, contentType, reqType mime.MimeType) (string, error)
	GetMemoryLimitBytes(podName string) (uint64, error)
	GetMaxMemoryUnboundedBytes(podName string) (uint64, error)
	CacheNames(podName string) ([]string, error)
	GetMetrics(podName, postfix string) (*bytes.Buffer, error)
	GetCacheManagerInfo(cacheManagerName, podName string) (*CacheManagerInfo, error)
	GetLoggers(podName string) (map[string]string, error)
	SetLogger(podName, loggerName, loggerLevel string) error
	XsitePushAllState(podName string) error
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
	rsp, err, reason := c.Client.Post(podName, consts.ServerHTTPContainerShutdown, "", nil)
	return validateResponse(rsp, reason, err, "during graceful shutdown", http.StatusNoContent)
}

// GracefulShutdownTask (ISPN-13141) uploads custom task to perform graceful shutdown that does not fail on cache errors
// This task calls Cache#shutdown which disables rebalancing on the cache before stopping it
func (c Cluster) GracefulShutdownTask(podName string) error {
	scriptName := "___org.infinispan.operator.gracefulshutdown.js"
	url := fmt.Sprintf("%s/tasks/%s", consts.ServerHTTPBasePath, scriptName)
	headers := map[string]string{"Content-Type": string(mime.TextPlain)}
	task := `
	/* mode=local,language=javascript */
	print("Executing operator shutdown task");
	var System = Java.type("java.lang.System");
	var HashSet = Java.type("java.util.HashSet");
	var LinkedHashSet = Java.type("java.util.LinkedHashSet");

	var stdErr = System.err;
	var gcr = cacheManager.getGlobalComponentRegistry();
	var icr = gcr.getComponent("org.infinispan.registry.InternalCacheRegistry");
	var cacheDependencyGraph = gcr.getComponent("org.infinispan.CacheDependencyGraph");

	var cachesToShutdown = new LinkedHashSet();
	cachesToShutdown.addAll(cacheDependencyGraph.topologicalSort());

	cachesToShutdown.addAll(icr.getInternalCacheNames());
	cachesToShutdown.addAll(cacheManager.getCacheNames());
	shutdown(cachesToShutdown);

	function shutdown(cacheNames) {
		var it = cacheNames.iterator();
		while (it.hasNext()) {
			name = it.next();
			print("Shutting down cache " + name);
			try {
				cacheManager.getCache(name).shutdown();
			} catch (err) {
				stdErr.println("Encountered error trying to shutdown cache " + name + ": " + err);
			}
		}
	}`
	// Remove all new lines to prevent a "100 continue" response
	task = strings.ReplaceAll(task, "\n", "")
	task = strings.ReplaceAll(task, "\t", "")

	rsp, err, reason := c.Client.Post(podName, url, task, headers)
	if err := validateResponse(rsp, reason, err, "Uploading GracefulShutdownTask", http.StatusOK); err != nil {
		return err
	}

	rsp, err, reason = c.Client.Post(podName, url+"?action=exec", "", nil)
	return validateResponse(rsp, reason, err, "Executing GracefulShutdownTask", http.StatusOK)
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

func (c Cluster) DeleteCache(cacheName, podName string) error {
	path := fmt.Sprintf("%s/caches/%s", consts.ServerHTTPBasePath, cacheName)
	rsp, err, reason := c.Client.Delete(podName, path, nil)
	return validateResponse(rsp, reason, err, "deleting cache", http.StatusOK, http.StatusNotFound)
}

// UpdateCacheWithConfiguration update cache on the pod `podName`
func (c Cluster) UpdateCacheWithConfiguration(cacheName, config, podName string, mimeType mime.MimeType) error {
	headers := make(map[string]string)
	headers["Content-Type"] = string(mimeType)

	path := fmt.Sprintf("%s/caches/%s", consts.ServerHTTPBasePath, cacheName)
	rsp, err, reason := c.Client.Put(podName, path, config, headers)
	return validateResponse(rsp, reason, err, "updating cache", http.StatusOK)
}

// CreateCacheWithConfiguration create cache on the pod `podName`
func (c Cluster) CreateCacheWithConfiguration(cacheName, config, podName string, mimeType mime.MimeType) error {
	headers := make(map[string]string)
	headers["Content-Type"] = string(mimeType)

	path := fmt.Sprintf("%s/caches/%s", consts.ServerHTTPBasePath, cacheName)
	rsp, err, reason := c.Client.Post(podName, path, config, headers)
	return validateResponse(rsp, reason, err, "creating cache", http.StatusOK)
}

// CreateCacheWithTemplateName create cache on the pod `podName`
func (c Cluster) CreateCacheWithTemplateName(cacheName, templateName, podName string) error {
	path := fmt.Sprintf("%s/caches/%s?template=%s", consts.ServerHTTPBasePath, cacheName, templateName)
	rsp, err, reason := c.Client.Post(podName, path, "", nil)
	return validateResponse(rsp, reason, err, "creating cache with template", http.StatusOK)
}

// CreateCache create cache on the pod `podName`
func (c Cluster) CreateCache(cacheName, jsonConfig, podName string) error {
	path := fmt.Sprintf("%s/caches/%s", consts.ServerHTTPBasePath, cacheName)
	rsp, err, reason := c.Client.Post(podName, path, jsonConfig, Headers{"Content-Type": "application/json"})
	return validateResponse(rsp, reason, err, "creating cache", http.StatusOK)
}

// GetCacheConfig returns the cache configuration
func (c Cluster) GetCacheConfig(cacheName, podName string) (cacheConfig string, err error) {
	path := fmt.Sprintf("%s/caches/%s?action=config", consts.ServerHTTPBasePath, cacheName)
	rsp, err, reason := c.Client.Get(podName, path, nil)
	if err = validateResponse(rsp, reason, err, "getting cache config", http.StatusOK); err != nil {
		return
	}
	defer func() {
		cerr := rsp.Body.Close()
		if err == nil {
			err = cerr
		}
	}()
	var body json.RawMessage
	if err = json.NewDecoder(rsp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("unable to decode: %w", err)
	}
	return string(body), nil
}

// AddRemoteStore connects the target cluster to the source cluster using a Remote Store
func (c Cluster) AddRemoteStore(cacheName, jsonConfig, podName string) error {
	path := fmt.Sprintf("%s/caches/%s/rolling-upgrade/source-connection", consts.ServerHTTPBasePath, cacheName)
	rsp, err, reason := c.Client.Post(podName, path, jsonConfig, Headers{"Content-Type": "application/json"})
	defer func() {
		cerr := rsp.Body.Close()
		if err == nil {
			err = cerr
		}
	}()
	return validateResponse(rsp, reason, err, "add remote store", http.StatusNoContent)
}

// IsSourceConnected returns true if the cache is connected to a remote cluster
func (c Cluster) IsSourceConnected(cacheName, podName string) (bool, error) {
	path := fmt.Sprintf("%s/caches/%s/rolling-upgrade/source-connection", consts.ServerHTTPBasePath, cacheName)
	rsp, err, reason := c.Client.Head(podName, path, nil)
	defer func() {
		if rsp != nil {
			cerr := rsp.Body.Close()
			if err == nil {
				err = cerr
			}
		}
	}()

	if err := validateResponse(rsp, reason, err, "source-connected", http.StatusOK, http.StatusNotFound); err != nil {
		return false, err
	}
	switch rsp.StatusCode {
	case http.StatusOK:
		return true, nil
	default:
		return false, nil
	}
}

// DisconnectSource disconnect the target cluster from the source cluster
func (c Cluster) DisconnectSource(cacheName, podName string) error {
	path := fmt.Sprintf("%s/caches/%s/rolling-upgrade/source-connection", consts.ServerHTTPBasePath, cacheName)
	rsp, err, reason := c.Client.Delete(podName, path, nil)
	return validateResponse(rsp, reason, err, "disconnect source", http.StatusNoContent)
}

// SyncData Pulls data from the source cluster and write it to the target cluster
func (c Cluster) SyncData(cacheName, podName string) (string, error) {
	path := fmt.Sprintf("%s/caches/%s?action=sync-data", consts.ServerHTTPBasePath, cacheName)
	rsp, err, reason := c.Client.Post(podName, path, "", nil)

	defer func() {
		cerr := rsp.Body.Close()
		if err == nil {
			err = cerr
		}
	}()

	if err = validateResponse(rsp, reason, err, "syncing data", http.StatusOK); err != nil {
		return "", err
	}

	all, err := ioutil.ReadAll(rsp.Body)

	if err != nil {
		return string(all), fmt.Errorf("unable to decode: %w", err)
	}
	return string(all), nil
}

func (c Cluster) ConvertCacheConfiguration(config, podName string, contentType, reqType mime.MimeType) (string, error) {
	path := consts.ServerHTTPBasePath + "/caches?action=convert"
	headers := map[string]string{
		"Accept":       string(reqType),
		"Content-Type": string(contentType),
	}
	rsp, err, reason := c.Client.Post(podName, path, config, headers)
	err = validateResponse(rsp, reason, err, "creating cache with template", http.StatusOK)
	if err != nil {
		return "", err
	}
	responseBody, responseErr := ioutil.ReadAll(rsp.Body)
	if responseErr != nil {
		return "", fmt.Errorf("unable to read response body: %w", responseErr)
	}
	return string(responseBody), nil
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
	headers["Accept"] = string(mime.ApplicationJson)

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

func (c Cluster) XsitePushAllState(podName string) (err error) {
	rsp, err, reason := c.Client.Get(podName, consts.ServerHTTPXSitePath, nil)
	if err = validateResponse(rsp, reason, err, "Retrieving xsite status", http.StatusOK); err != nil {
		return
	}

	defer func() {
		cerr := rsp.Body.Close()
		if err == nil {
			err = cerr
		}
	}()

	type xsiteStatus struct {
		Status string `json:"status"`
	}
	var statuses map[string]xsiteStatus
	if err := json.NewDecoder(rsp.Body).Decode(&statuses); err != nil {
		return fmt.Errorf("unable to decode: %w", err)
	}

	// Statuses will be empty if no xsite caches are configured
	for k, v := range statuses {
		if v.Status == "online" {
			url := fmt.Sprintf("%s/%s?action=start-push-state", consts.ServerHTTPXSitePath, k)
			rsp, err, reason = c.Client.Post(podName, url, "", nil)
			if err = validateResponse(rsp, reason, err, "Pushing xsite state", http.StatusOK); err != nil {
				return
			}
		}
	}
	return
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
