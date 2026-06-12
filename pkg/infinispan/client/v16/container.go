package v16

import (
	httpClient "github.com/infinispan/infinispan-operator/pkg/http"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
	v14 "github.com/infinispan/infinispan-operator/pkg/infinispan/client/v14"
	v15 "github.com/infinispan/infinispan-operator/pkg/infinispan/client/v15"
)

type Container struct {
	*v14.Container
}

func NewContainer(client httpClient.HttpClient) api.Container {
	return &Container{
		Container: &v14.Container{
			PathResolver: v15.NewPathResolver(),
			HttpClient:   client,
		},
	}
}

func (c *Container) Members() (members []string, err error) {
	info, err := c.Info()
	if err != nil {
		return
	}
	return info.ClusterMembers, nil
}
