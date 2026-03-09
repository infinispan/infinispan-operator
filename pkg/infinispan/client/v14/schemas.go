package v14

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	httpClient "github.com/infinispan/infinispan-operator/pkg/http"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
)

type protoSchema struct {
	api.PathResolver
	httpClient.HttpClient
	name string
}

type protoSchemas struct {
	api.PathResolver
	httpClient.HttpClient
}

func (s *protoSchema) url() string {
	return fmt.Sprintf("%s/%s", s.Schemas(""), url.PathEscape(s.name))
}

func (s *protoSchema) Create(schema string) (response *api.SchemaResponse, err error) {
	headers := map[string]string{
		"Content-Type": "text/plain",
	}
	rsp, err := s.Post(s.url(), schema, headers)
	defer func() {
		err = httpClient.CloseBody(rsp, err)
	}()
	if err = httpClient.ValidateResponse(rsp, err, "creating schema", http.StatusOK, http.StatusCreated, http.StatusConflict); err != nil {
		return
	}
	response = &api.SchemaResponse{}
	if err = json.NewDecoder(rsp.Body).Decode(response); err != nil {
		return nil, fmt.Errorf("unable to decode schema response: %w", err)
	}
	return
}

func (s *protoSchema) CreateOrUpdate(schema string) (response *api.SchemaResponse, err error) {
	headers := map[string]string{
		"Content-Type": "text/plain",
	}
	rsp, err := s.Put(s.url(), schema, headers)
	defer func() {
		err = httpClient.CloseBody(rsp, err)
	}()
	if err = httpClient.ValidateResponse(rsp, err, "updating schema", http.StatusOK); err != nil {
		return
	}
	response = &api.SchemaResponse{}
	if err = json.NewDecoder(rsp.Body).Decode(response); err != nil {
		return nil, fmt.Errorf("unable to decode schema response: %w", err)
	}
	return
}

func (s *protoSchema) Delete() (err error) {
	rsp, err := s.HttpClient.Delete(s.url(), nil)
	defer func() {
		err = httpClient.CloseBody(rsp, err)
	}()
	err = httpClient.ValidateResponse(rsp, err, "deleting schema", http.StatusNoContent, http.StatusNotFound)
	return
}

func (s *protoSchema) Get() (schema string, err error) {
	rsp, err := s.HttpClient.Get(s.url(), nil)
	defer func() {
		err = httpClient.CloseBody(rsp, err)
	}()
	if err = httpClient.ValidateResponse(rsp, err, "getting schema", http.StatusOK, http.StatusNotFound); err != nil {
		return
	}
	if rsp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	return readResponseBody(rsp)
}

func (s *protoSchemas) Names() (schemas []api.SchemaResponse, err error) {
	rsp, err := s.HttpClient.Get(s.Schemas(""), nil)
	if err = httpClient.ValidateResponse(rsp, err, "listing schemas", http.StatusOK); err != nil {
		return
	}
	defer func() {
		err = httpClient.CloseBody(rsp, err)
	}()
	if err = json.NewDecoder(rsp.Body).Decode(&schemas); err != nil {
		return nil, fmt.Errorf("unable to decode: %w", err)
	}
	return
}
