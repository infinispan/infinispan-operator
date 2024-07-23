package v14

import "github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"

func NewPathResolver() api.PathResolver {
	return &pathResolver{Root: "rest/v2"}
}

type pathResolver struct {
	Root string
}

func (r *pathResolver) Caches(s string) string {
	return r.Root + "/caches" + s
}

func (r *pathResolver) CacheManager(s string) string {
	return r.Root + "/cache-managers/default" + s
}

func (r *pathResolver) Container(s string) string {
	return r.Root + "/container" + s
}

func (r *pathResolver) Logging(s string) string {
	return r.Root + "/logging/loggers" + s
}

func (r *pathResolver) Server(s string) string {
	return r.Root + "/server" + s
}
