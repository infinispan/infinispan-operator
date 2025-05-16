package v14

import (
	"github.com/infinispan/infinispan-operator/pkg/http"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/version"
)

type infinispan struct {
	api.PathResolver
	http.HttpClient
	operand version.Operand
}

func New(operand version.Operand, client http.HttpClient) api.Infinispan {
	return NewWithPathResolver(operand, client, NewPathResolver())
}

func NewWithPathResolver(operand version.Operand, client http.HttpClient, pathResolver api.PathResolver) api.Infinispan {
	return &infinispan{pathResolver, client, operand}
}

func (i *infinispan) Cache(name string) api.Cache {
	return &cache{i.PathResolver, i.HttpClient, name}
}

func (i *infinispan) Caches() api.Caches {
	return &caches{i.PathResolver, i.HttpClient}
}

func (i *infinispan) Container() api.Container {
	return &Container{i.PathResolver, i.HttpClient}
}

func (i *infinispan) Logging() api.Logging {
	return &logging{i.PathResolver, i.HttpClient}
}

func (i *infinispan) Metrics() api.Metrics {
	return &metrics{i.HttpClient}
}

func (i *infinispan) ProtobufMetadataCacheName() string {
	return "___protobuf_metadata"
}

func (i *infinispan) ScriptCacheName() string {
	return "___script_cache"
}

func (i *infinispan) Server() api.Server {
	return &server{i.PathResolver, i.HttpClient}
}

func (i *infinispan) Version() version.Operand {
	return i.operand
}
