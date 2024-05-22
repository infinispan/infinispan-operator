// Package v13 implements a client for interacting with Infinispan 13.x servers
package v13

import (
	"github.com/infinispan/infinispan-operator/pkg/http"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
)

const (
	MajorVersion = "13"
)

type infinispan struct {
	api.PathResolver
	http.HttpClient
}

func New(client http.HttpClient) api.Infinispan {
	return NewWithPathResolver(client, NewPathResolver())
}

func NewWithPathResolver(client http.HttpClient, pathResolver api.PathResolver) api.Infinispan {
	return &infinispan{pathResolver, client}
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
