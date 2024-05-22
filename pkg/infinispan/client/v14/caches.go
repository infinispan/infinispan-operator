package v14

import (
	"net/http"

	httpClient "github.com/infinispan/infinispan-operator/pkg/http"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
)

type caches struct {
	httpClient.HttpClient
	api.Caches
	api.PathResolver
}

func (c *caches) EqualConfiguration(a, b string) (bool, error) {
	path := c.PathResolver.Caches("?action=compare")
	parts := map[string]string{
		"a": a,
		"b": b,
	}
	rsp, err := c.PostMultipart(path, parts, nil)
	defer func() {
		err = httpClient.CloseBody(rsp, err)
	}()
	err = httpClient.ValidateResponse(rsp, err, "checking cache configuration equality", http.StatusNoContent, http.StatusConflict)
	if err != nil {
		return false, err
	}
	return rsp.StatusCode == http.StatusNoContent, nil
}
