package v2alpha1

import (
	v1 "github.com/infinispan/infinispan-operator/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BackupSpec defines the desired state of Backup
type RestoreSpec struct {
	// Infinispan cluster name
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Cluster Name",xDescriptors="urn:alm:descriptor:io.kubernetes:infinispan.org:v1:Infinispan"
	Cluster string `json:"cluster"`
	// The Infinispan Backup to restore
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Backup Name",xDescriptors="urn:alm:descriptor:io.kubernetes:infinispan.org:v2alpha1:Backup"
	Backup string `json:"backup"`
	// +optional
	Resources *RestoreResources `json:"resources,omitempty"`
	// +optional
	Container v1.InfinispanContainerSpec `json:"container,omitempty"`
}

type RestoreResources struct {
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

type RestorePhase string

const (
	// RestoreInitializing means the request has been accepted by the system, but the underlying resources are still
	// being initialized.
	RestoreInitializing RestorePhase = "Initializing"
	// RestoreInitialized means that all required resources have been initialized.
	RestoreInitialized RestorePhase = "Initialized"
	// RestoreRunning means that the Restore pod has been created and the Restore process initiated on the infinispan server.
	RestoreRunning RestorePhase = "Running"
	// RestoreSucceeded means that the Restore process on the server has completed and the Restore pod has been terminated.
	RestoreSucceeded RestorePhase = "Succeeded"
	// RestoreFailed means that the Restore failed on the infinispan server and the Restore pod has terminated.
	RestoreFailed RestorePhase = "Failed"
	// RestoreUnknown means that for some reason the state of the Restore could not be obtained, typically due
	// to an error in communicating with the underlying Restore pod.
	RestoreUnknown RestorePhase = "Unknown"
)

// RestoreStatus defines the observed state of Restore
type RestoreStatus struct {
	// Current phase of the restore operation
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Phase"
	Phase RestorePhase `json:"phase"`
	// Reason indicates the reason for any restore related failures.
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Reason"
	Reason string `json:"reason,omitempty"`
}

// +kubebuilder:object:root=true

// Restore is the Schema for the restores API
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=restores,scope=Namespaced
type Restore struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RestoreSpec   `json:"spec,omitempty"`
	Status RestoreStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// RestoreList contains a list of Restore
type RestoreList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Restore `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Restore{}, &RestoreList{})
}
