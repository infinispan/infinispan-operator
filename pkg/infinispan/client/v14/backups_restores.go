package v14

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	inputValidator "github.com/go-playground/validator/v10"
	httpClient "github.com/infinispan/infinispan-operator/pkg/http"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
	"github.com/infinispan/infinispan-operator/pkg/mime"
)

var validator = inputValidator.New()

type backups struct {
	api.PathResolver
	httpClient.HttpClient
}

type restores struct {
	api.PathResolver
	httpClient.HttpClient
}

func (b backups) Create(name string, config *api.BackupConfig) (err error) {
	return create(b.backup(name), name, "backup", config, b)
}

func (b backups) Status(name string) (api.Status, error) {
	return status(b.backup(name), name, "Backup", b)
}

func (r restores) Create(name string, config *api.RestoreConfig) (err error) {
	return create(r.restore(name), name, "restore", config, r)
}

func (r restores) Status(name string) (api.Status, error) {
	return status(r.restore(name), name, "Restore", r)
}

func (b backups) backup(name string) string {
	return b.CacheManager("/backups/" + name)
}

func (r restores) restore(name string) string {
	return r.CacheManager("/restores/" + name)
}

func create(url, name, op string, config interface{}, client httpClient.HttpClient) (err error) {
	if err := validator.Var(name, "required"); err != nil {
		return err
	}
	if err := validator.Struct(config); err != nil {
		return err
	}

	headers := map[string]string{"Content-Type": string(mime.ApplicationJson)}
	json, err := json.Marshal(config)
	if err != nil {
		return err
	}

	payload := string(json)
	rsp, err := client.Post(url, payload, headers)
	defer func() {
		err = httpClient.CloseBody(rsp, err)
	}()
	return httpClient.ValidateResponse(rsp, err, "creating "+op, http.StatusAccepted)
}

func status(url, name, op string, client httpClient.HttpClient) (api.Status, error) {
	if err := validator.Var(name, "required"); err != nil {
		return api.StatusUnknown, err
	}

	rsp, err := client.Head(url, nil)
	defer func() {
		err = httpClient.CloseBody(rsp, err)
	}()
	if err != nil {
		return api.StatusUnknown, err
	}

	bodyOrStatus := func(rsp *http.Response) interface{} {
		if body, err := io.ReadAll(rsp.Body); err != nil || string(body) == "" {
			return rsp.Status
		} else {
			return body
		}
	}

	switch rsp.StatusCode {
	case http.StatusOK:
		fallthrough
	case http.StatusCreated:
		return api.StatusSucceeded, nil
	case http.StatusAccepted:
		return api.StatusRunning, nil
	case http.StatusNotFound:
		return api.StatusNotFound, nil
	case http.StatusInternalServerError:
		return api.StatusFailed, fmt.Errorf("unable to retrieve %s with name '%s' due to server error: '%s'", op, name, bodyOrStatus(rsp))
	default:
		return api.StatusUnknown, fmt.Errorf("%s failed. Unexpected response %d: '%s'", op, rsp.StatusCode, bodyOrStatus(rsp))
	}
}
