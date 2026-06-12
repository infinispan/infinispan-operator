package v16

import (
	"github.com/infinispan/infinispan-operator/pkg/http"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
	v15 "github.com/infinispan/infinispan-operator/pkg/infinispan/client/v15"
)

type infinispan struct {
	http.HttpClient
	api.Infinispan
}

func New(client http.HttpClient) api.Infinispan {
	return &infinispan{
		HttpClient: client,
		Infinispan: v15.New(client),
	}
}

func (i *infinispan) Caches() api.Caches {
	return NewCaches(i.HttpClient)
}

func (i *infinispan) Container() api.Container {
	return NewContainer(i.HttpClient)
}
