package v13

import (
	"net/http"

	httpClient "github.com/infinispan/infinispan-operator/pkg/http"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
)

type server struct {
	api.PathResolver
	httpClient.HttpClient
}

func (s *server) Stop() (err error) {
	rsp, err := s.Post(s.Server("?action=stop"), "", nil)
	defer func() {
		err = httpClient.CloseBody(rsp, err)
	}()
	err = httpClient.ValidateResponse(rsp, err, "stopping server", http.StatusNoContent)
	return
}
