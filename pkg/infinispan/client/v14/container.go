package v14

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	httpClient "github.com/infinispan/infinispan-operator/pkg/http"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
)

type Container struct {
	api.PathResolver
	httpClient.HttpClient
}

func (c *Container) Info() (info *api.ContainerInfo, err error) {
	rsp, err := c.Get(c.CacheManager(""), nil)
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

func (c *Container) HealthStatus() (status api.HealthStatus, err error) {
	rsp, err := c.Get(c.CacheManager("/health/status"), nil)
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

func (c *Container) Members() (members []string, err error) {
	rsp, err := c.Get(c.CacheManager("/health"), nil)
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

func (c *Container) Backups() api.Backups {
	return &backups{c.PathResolver, c.HttpClient}
}

func (c *Container) RebalanceDisable() error {
	return c.rebalance("disable-rebalancing")
}

func (c *Container) RebalanceEnable() error {
	return c.rebalance("enable-rebalancing")
}

func (c *Container) rebalance(action string) error {
	rsp, err := c.Post(c.CacheManager("?action="+action), "", nil)
	defer func() {
		err = httpClient.CloseBody(rsp, err)
	}()
	err = httpClient.ValidateResponse(rsp, err, fmt.Sprintf("during %s", action), http.StatusNoContent)
	return err
}

func (c *Container) Restores() api.Restores {
	return &restores{c.PathResolver, c.HttpClient}
}

func (c *Container) Shutdown() (err error) {
	rsp, err := c.Post(c.Container("?action=shutdown"), "", nil)
	defer func() {
		err = httpClient.CloseBody(rsp, err)
	}()
	err = httpClient.ValidateResponse(rsp, err, "during graceful shutdown", http.StatusNoContent)
	return err
}

func (c *Container) Xsite() api.Xsite {
	return &xsite{c.HttpClient, c.PathResolver}
}
