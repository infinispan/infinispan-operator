package v16

import (
	"encoding/json"
	"fmt"
	"net/http"

	httpClient "github.com/infinispan/infinispan-operator/pkg/http"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
	v14 "github.com/infinispan/infinispan-operator/pkg/infinispan/client/v14"
	v15 "github.com/infinispan/infinispan-operator/pkg/infinispan/client/v15"
)

type Caches struct {
	*v14.Caches
}

func NewCaches(client httpClient.HttpClient) api.Caches {
	return &Caches{
		Caches: &v14.Caches{
			PathResolver: v15.NewPathResolver(),
			HttpClient:   client,
		},
	}
}

func (c *Caches) Detailed() (cacheHealth []api.CacheHealth, err error) {
	rsp, err := c.Get(c.PathResolver.Caches("?action=detailed"), nil)
	defer func() {
		err = httpClient.CloseBody(rsp, err)
	}()

	if err = httpClient.ValidateResponse(rsp, err, "getting detailed caches info", http.StatusOK); err != nil {
		return
	}

	var caches []struct {
		Name   string           `json:"name"`
		Status string           `json:"status"`
		Health api.HealthStatus `json:"health"`
	}
	if err = json.NewDecoder(rsp.Body).Decode(&caches); err != nil {
		return nil, fmt.Errorf("unable to decode caches: %w", err)
	}

	cacheHealth = make([]api.CacheHealth, len(caches))
	for i, cache := range caches {
		cacheHealth[i] = api.CacheHealth{
			Name:   cache.Name,
			Status: cache.Health,
		}
	}

	return
}
