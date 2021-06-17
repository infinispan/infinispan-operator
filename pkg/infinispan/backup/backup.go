package backup

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/go-playground/validator/v10"
	client "github.com/infinispan/infinispan-operator/pkg/infinispan/client/http"
)

type BackupConfig struct {
	Directory string `json:"directory" validate:"required"`
	// +optional
	Resources Resources `json:"resources,omitempty"`
}

type RestoreConfig struct {
	Location string `json:"location" validate:"required"`
	// +optional
	Resources Resources `json:"resources,omitempty"`
}

type Resources struct {
	// +optional
	Caches []string `json:"caches,omitempty"`
	// +optional
	Templates []string `json:"templates,omitempty"`
	// +optional
	Counters []string `json:"counters,omitempty"`
	// +optional
	ProtoSchemas []string `json:"proto-schemas,omitempty"`
	// +optional
	Tasks []string `json:"tasks,omitempty"`
}

type Status string

const (
	// StatusSucceeded means that the backup process on the server has completed.
	StatusSucceeded Status = "Succeeded"
	// StatusRunning means that the backup is currently in progress on the infinispan server.
	StatusRunning Status = "Running"
	// StatusFailed means that the creation of backup failed on the infinispan server.
	StatusFailed Status = "Failed"
	// StatusUnknown means that the state of the backup could not be obtained, typically due to an error in communicating with the infinispan server.
	StatusUnknown Status = "Unknown"
)

type ManagerInterface interface {
	Backup(name string, config *BackupConfig) error
	BackupStatus(name string) (Status, error)
	Restore(name string, config *RestoreConfig) error
	RestoreStatus(name string) (Status, error)
}

type Manager struct {
	http      client.HttpClient
	podName   string
	validator *validator.Validate
}

const (
	baseUrl    = "rest/v2/cache-managers/default"
	BackupUrl  = baseUrl + "/backups"
	RestoreUrl = baseUrl + "/restores"
)

// NewManager creates a new instance of BackupManager
func NewManager(podName string, http client.HttpClient) *Manager {
	return &Manager{
		http:      http,
		podName:   podName,
		validator: validator.New(),
	}
}

func (manager *Manager) Backup(name string, config *BackupConfig) error {
	if err := manager.validator.Var(name, "required"); err != nil {
		return err
	}
	if err := manager.validator.Struct(config); err != nil {
		return err
	}
	url := fmt.Sprintf("%s/%s", BackupUrl, name)
	return manager.post(url, "Backup", config)
}

func (manager *Manager) BackupStatus(name string) (Status, error) {
	url := fmt.Sprintf("%s/%s", BackupUrl, name)
	return manager.status(url, name, "Backup")
}

func (manager *Manager) Restore(name string, config *RestoreConfig) error {
	if err := manager.validator.Var(name, "required"); err != nil {
		return err
	}
	if err := manager.validator.Struct(config); err != nil {
		return err
	}
	url := fmt.Sprintf("%s/%s", RestoreUrl, name)
	return manager.post(url, "Restore", config)
}

func (manager *Manager) RestoreStatus(name string) (Status, error) {
	url := fmt.Sprintf("%s/%s", RestoreUrl, name)
	return manager.status(url, name, "Restore")
}

func (manager *Manager) post(url, op string, config interface{}) (err error) {
	headers := map[string]string{"Content-Type": "application/json"}
	json, err := json.Marshal(config)
	if err != nil {
		return
	}

	payload := string(json)
	rsp, err, _ := manager.http.Post(manager.podName, url, payload, headers)
	if err != nil {
		return err
	}

	defer func() {
		cerr := rsp.Body.Close()
		if err == nil {
			err = cerr
		}
	}()

	if rsp.StatusCode == http.StatusAccepted {
		return
	}
	return fmt.Errorf("%s failed. Unexpected response %d: '%s'", op, rsp.StatusCode, rsp.Body)
}

func (manager *Manager) status(url, name, op string) (Status, error) {
	if err := manager.validator.Var(name, "required"); err != nil {
		return StatusUnknown, err
	}

	rsp, err, _ := manager.http.Head(manager.podName, url, nil)
	if err != nil {
		return StatusUnknown, err
	}

	switch rsp.StatusCode {
	case http.StatusOK:
		fallthrough
	case http.StatusCreated:
		return StatusSucceeded, nil
	case http.StatusAccepted:
		return StatusRunning, nil
	case http.StatusNotFound:
		return StatusUnknown, fmt.Errorf("Unable to retrieve %s with name '%s' from the server", op, name)
	case http.StatusInternalServerError:
		return StatusFailed, fmt.Errorf("Unable to retrieve %s with name '%s' due to server error: '%s'", op, name, bodyOrStatus(rsp))
	default:
		return StatusUnknown, fmt.Errorf("%s failed. Unexpected response %d: '%s'", op, rsp.StatusCode, bodyOrStatus(rsp))
	}
}

func bodyOrStatus(rsp *http.Response) interface{} {
	if body, err := ioutil.ReadAll(rsp.Body); err != nil || string(body) == "" {
		return rsp.Status
	}
	return rsp.Body
}
