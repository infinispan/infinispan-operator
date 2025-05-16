package v15

import (
	"github.com/infinispan/infinispan-operator/pkg/http"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
	v14 "github.com/infinispan/infinispan-operator/pkg/infinispan/client/v14"
)

type infinispan struct {
	http.HttpClient
	api.Infinispan
}

func New(client http.HttpClient) api.Infinispan {
	return &infinispan{
		HttpClient: client,
		Infinispan: v14.NewWithPathResolver(client, NewPathResolver()),
	}
}
