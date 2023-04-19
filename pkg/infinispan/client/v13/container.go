package v13

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	httpClient "github.com/infinispan/infinispan-operator/pkg/http"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
	"github.com/infinispan/infinispan-operator/pkg/mime"
)

const (
	CacheManagerPath = BasePath + "/cache-managers/default"
	ContainerPath    = BasePath + "/container"
	HealthPath       = CacheManagerPath + "/health"
	HealthStatusPath = HealthPath + "/status"
)

type container struct {
	httpClient.HttpClient
}

func (c *container) Info() (info *api.ContainerInfo, err error) {
	rsp, err := c.Get(CacheManagerPath, nil)
	defer func() {
		err = httpClient.CloseBody(rsp, err)
	}()

	if err = httpClient.ValidateResponse(rsp, err, "getting cache manager info", http.StatusOK); err != nil {
		return
	}

	if err = json.NewDecoder(rsp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("unable to decode: %w", err)
	}
	return
}

func (c *container) HealthStatus() (status api.HealthStatus, err error) {
	rsp, err := c.Get(HealthStatusPath, nil)
	defer func() {
		err = httpClient.CloseBody(rsp, err)
	}()

	if err = httpClient.ValidateResponse(rsp, err, "getting cache manager health status", http.StatusOK); err != nil {
		return
	}
	all, err := io.ReadAll(rsp.Body)
	if err != nil {
		return "", fmt.Errorf("unable to decode: %w", err)
	}
	return api.HealthStatus(string(all)), nil
}

func (c *container) Members() (members []string, err error) {
	rsp, err := c.Get(HealthPath, nil)
	defer func() {
		err = httpClient.CloseBody(rsp, err)
	}()

	if err = httpClient.ValidateResponse(rsp, err, "getting cluster members", http.StatusOK); err != nil {
		return
	}

	type Health struct {
		ClusterHealth struct {
			Nodes []string `json:"node_names"`
		} `json:"cluster_health"`
	}

	var health Health
	if err := json.NewDecoder(rsp.Body).Decode(&health); err != nil {
		return nil, fmt.Errorf("unable to decode: %w", err)
	}
	return health.ClusterHealth.Nodes, nil
}

func (c *container) Backups() api.Backups {
	return &backups{c.HttpClient}
}

func (c *container) Restores() api.Restores {
	return &restores{c.HttpClient}
}

func (c *container) Shutdown() (err error) {
	rsp, err := c.Post(ContainerPath+"?action=shutdown", "", nil)
	defer func() {
		err = httpClient.CloseBody(rsp, err)
	}()
	err = httpClient.ValidateResponse(rsp, err, "during graceful shutdown", http.StatusNoContent)
	return err
}

// ShutdownTask (ISPN-13141) uploads custom task to perform graceful shutdown that does not fail on cache errors
// This task calls Cache#shutdown which disables rebalancing on the cache before stopping it
func (c *container) ShutdownTask() (err error) {
	scriptName := "___org.infinispan.operator.gracefulshutdown.js"
	url := fmt.Sprintf("%s/tasks/%s", BasePath, scriptName)
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

	rsp, err := c.Post(url, task, headers)
	defer func() {
		err = httpClient.CloseBody(rsp, err)
	}()
	if err = httpClient.ValidateResponse(rsp, err, "Uploading GracefulShutdownTask", http.StatusOK); err != nil {
		return
	}

	rsp, err = c.Post(url+"?action=exec", "", nil)
	defer func() {
		err = httpClient.CloseBody(rsp, err)
	}()
	err = httpClient.ValidateResponse(rsp, err, "Executing GracefulShutdownTask", http.StatusOK)
	return
}

func (c *container) Xsite() api.Xsite {
	return &xsite{c.HttpClient}
}
