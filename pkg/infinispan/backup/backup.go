package backup

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-playground/validator/v10"
	client "github.com/infinispan/infinispan-operator/pkg/infinispan/client/http"
)

type BackupConfig struct {
	Directory string    `json:"directory" validate:"required"`
	Resources Resources `json:"resources,optional,omitempty"`
}

type RestoreConfig struct {
	Location  string    `json:"location" validate:"required"`
	Resources Resources `json:"resources,optional,omitempty"`
}

type Resources struct {
	Caches       []string `json:"caches,optional,omitempty"`
	CacheConfigs []string `json:"cache-configs,optional,omitempty"`
	Counters     []string `json:"counters,optional,omitempty"`
	ProtoSchemas []string `json:"proto-schemas,optional,omitempty"`
	Scripts      []string `json:"scripts,optional,omitempty"`
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

// New creates a new instance of BackupManager
func NewManager(podName string, http client.HttpClient) *Manager {
	return &Manager{
		http:      http,
		podName:   podName,
		validator: validator.New(),
	}
}

func (manager *Manager) Backup(name string, config *BackupConfig) error {
	manager.validator.Var(name, "required")
	manager.validator.Struct(config)
	url := fmt.Sprintf("%s/%s", BackupUrl, name)
	return manager.post(url, "Backup", config)
}

func (manager *Manager) BackupStatus(name string) (Status, error) {
	url := fmt.Sprintf("%s/%s", BackupUrl, name)
	return manager.status(url, name, "Backup")
}

func (manager *Manager) Restore(name string, config *RestoreConfig) error {
	manager.validator.Var(name, "required")
	manager.validator.Struct(config)
	url := fmt.Sprintf("%s/%s", RestoreUrl, name)
	return manager.post(url, "Restore", config)
}

func (manager *Manager) RestoreStatus(name string) (Status, error) {
	url := fmt.Sprintf("%s/%s", RestoreUrl, name)
	return manager.status(url, name, "Restore")
}

func (manager *Manager) post(url, op string, config interface{}) error {
	headers := map[string]string{"Content-Type": "application/json"}
	json, err := json.Marshal(config)
	if err != nil {
		return err
	}

	payload := string(json)
	rsp, err, _ := manager.http.Post(manager.podName, url, payload, headers)
	if err != nil {
		return err
	}

	if rsp.StatusCode == http.StatusAccepted {
		return nil
	}
	return fmt.Errorf("%s failed. Unexpected response %d: '%s'", op, rsp.StatusCode, rsp.Body)
}

func (manager *Manager) status(url, name, op string) (Status, error) {
	manager.validator.Var(name, "required")

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
		return StatusUnknown, fmt.Errorf("Unable to find %s with name '%s'", op, name)
	case http.StatusInternalServerError:
		return StatusFailed, nil
	default:
		return StatusUnknown, fmt.Errorf("%s failed. Unexpected response %d: '%s'", op, rsp.StatusCode, rsp.Body)
	}
}
