package v15

import (
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/v14"
)

func NewPathResolver() api.PathResolver {
	return &pathResolver{v14.NewPathResolver()}
}

type pathResolver struct {
	api.PathResolver
}

func (r *pathResolver) CacheManager(s string) string {
	return r.Container(s)
}
