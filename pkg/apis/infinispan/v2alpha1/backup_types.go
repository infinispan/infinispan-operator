package v2alpha1

import (
	v1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BackupSpec defines the desired state of Backup
type BackupSpec struct {
	Cluster   string                     `json:"cluster"`
	Volume    BackupVolumeSpec           `json:"volume,optional,omitempty"`
	Resources *BackupResources           `json:"resources,optional,omitempty"`
	Container v1.InfinispanContainerSpec `json:"container,optional,omitempty"`
}

type BackupVolumeSpec struct {
	Storage          *string `json:"storage,optional,omitempty"`
	StorageClassName *string `json:"storageClassName,optional,omitempty"`
}

type BackupResources struct {
	Caches       []string `json:"caches,optional,omitempty"`
	Templates    []string `json:"templates,optional,omitempty"`
	Counters     []string `json:"counters,optional,omitempty"`
	ProtoSchemas []string `json:"protoSchemas,optional,omitempty"`
	Tasks        []string `json:"tasks,optional,omitempty"`

	// Deprecated and to be removed on subsequent release. Use .Templates instead.
	CacheConfigs []string `json:"cacheConfigs,optional,omitempty"`
	// Deprecated and to be removed on subsequent release. Use .Tasks instead.
	Scripts []string `json:"scripts,optional,omitempty"`
}

type BackupPhase string

const (
	// BackupInitializing means the request has been accepted by the system, but the underlying resources are still
	// being initialized.
	BackupInitializing BackupPhase = "Initializing"
	// BackupInitialized means that all required resources have been initialized
	BackupInitialized BackupPhase = "Initialized"
	// BackupRunning means that the backup pod has been created and the backup process initiated on the infinispan server.
	BackupRunning BackupPhase = "Running"
	// BackupSucceeded means that the backup process on the server has completed and the backup pod has been terminated.
	BackupSucceeded BackupPhase = "Succeeded"
	// BackupFailed means that the backup failed on the infinispan server and the backup pod has terminated.
	BackupFailed BackupPhase = "Failed"
	// BackupUnknown means that for some reason the state of the backup could not be obtained, typically due
	// to an error in communicating with the underlying backup pod.
	BackupUnknown BackupPhase = "Unknown"
)

// BackupStatus defines the observed state of Backup
type BackupStatus struct {
	// State indicates the current state of the backup operation
	Phase BackupPhase `json:"phase"`
	// Reason indicates the reason for any backup related failures.
	Reason string `json:"reason,omitempty"`
	// The name of the created PersistentVolumeClaim used to store the backup
	PVC string `json:"pvc,omitempty"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Backup is the Schema for the backups API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
type Backup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BackupSpec   `json:"spec,omitempty"`
	Status BackupStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// BackupList contains a list of Backup
type BackupList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Backup `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Backup{}, &BackupList{})
}

const (
	DefaultBackupCpuLimit = "500m"
	DefaultBackupMemory   = "512Mi"
)

func (s *BackupSpec) ApplyDefaults() {
	if s.Container.CPU == "" {
		s.Container.CPU = DefaultBackupCpuLimit
	}

	if s.Container.Memory == "" {
		s.Container.Memory = DefaultBackupMemory
	}
}
