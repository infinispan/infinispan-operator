package v15

import (
	"github.com/infinispan/infinispan-operator/pkg/http"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
	v14 "github.com/infinispan/infinispan-operator/pkg/infinispan/client/v14"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
)

type infinispan struct {
	http.HttpClient
	api.Infinispan
}

func New(operand version.Operand, client http.HttpClient) api.Infinispan {
	return &infinispan{
		HttpClient: client,
		Infinispan: v14.NewWithPathResolver(operand, client, NewPathResolver()),
	}
}
