package v15

import (
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
	v13 "github.com/infinispan/infinispan-operator/pkg/infinispan/client/v13"
)

func NewPathResolver() api.PathResolver {
	return &pathResolver{v13.NewPathResolver()}
}

type pathResolver struct {
	api.PathResolver
}

func (r *pathResolver) CacheManager(s string) string {
	return r.Container(s)
}
