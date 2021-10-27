package v2alpha1

import (
	v1 "github.com/infinispan/infinispan-operator/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BackupSpec defines the desired state of Backup
type BackupSpec struct {
	// Infinispan cluster name
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Cluster Name",xDescriptors="urn:alm:descriptor:io.kubernetes:infinispan.org:v1:Infinispan"
	Cluster string `json:"cluster"`
	// +optional
	Volume BackupVolumeSpec `json:"volume,omitempty"`
	// +optional
	Resources *BackupResources `json:"resources,omitempty"`
	// +optional
	Container v1.InfinispanContainerSpec `json:"container,omitempty"`
}

type BackupVolumeSpec struct {
	// +optional
	Storage *string `json:"storage,omitempty"`
	// +optional
	// Names the storage class object for persistent volume claims.
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Storage Class Name",xDescriptors="urn:alm:descriptor:io.kubernetes:StorageClass"
	StorageClassName *string `json:"storageClassName,omitempty"`
}

type BackupResources struct {
	// +optional
	Caches []string `json:"caches,omitempty"`
	// +optional
	Templates []string `json:"templates,omitempty"`
	// +optional
	Counters []string `json:"counters,omitempty"`
	// +optional
	ProtoSchemas []string `json:"protoSchemas,omitempty"`
	// +optional
	Tasks []string `json:"tasks,omitempty"`

	// Deprecated and to be removed on subsequent release. Use .Templates instead.
	// +optional
	CacheConfigs []string `json:"cacheConfigs,omitempty"`
	// Deprecated and to be removed on subsequent release. Use .Tasks instead.
	// +optional
	Scripts []string `json:"scripts,omitempty"`
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
	// Current phase of the backup operation
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Phase"
	Phase BackupPhase `json:"phase"`
	// Reason indicates the reason for any backup related failures.
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Reason"
	Reason string `json:"reason,omitempty"`
	// The name of the created PersistentVolumeClaim used to store the backup
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Persistent Volume Claim"
	PVC string `json:"pvc,omitempty"`
}

// +kubebuilder:object:root=true

// +kubebuilder:subresource:status
// +kubebuilder:resource:path=backups,scope=Namespaced
// Backup is the Schema for the backups API
type Backup struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BackupSpec   `json:"spec,omitempty"`
	Status BackupStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

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
