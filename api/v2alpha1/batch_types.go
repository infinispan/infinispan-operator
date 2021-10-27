package v2alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// BatchSpec defines the desired state of Batch
type BatchSpec struct {
	// Infinispan cluster name
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Cluster Name",xDescriptors="urn:alm:descriptor:io.kubernetes:infinispan.org:v1:Infinispan"
	Cluster string `json:"cluster"`
	// Batch string to be executed
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Config Command"
	Config *string `json:"config,omitempty"`
	// Name of the ConfigMap containing the batch and resource files to be executed
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="ConfigMap Name"
	ConfigMap *string `json:"configMap,omitempty"`
}

type BatchPhase string

const (
	// BatchInitializing means the request has been accepted by the system, but the underlying resources are still
	// being initialized.
	BatchInitializing BatchPhase = "Initializing"
	// BatchInitialized means that all required resources have been initialized
	BatchInitialized BatchPhase = "Initialized"
	// BatchRunning means that the Batch job has been created and the Batch process initiated on the infinispan server.
	BatchRunning BatchPhase = "Running"
	// BatchSucceeded means that the Batch job has completed successfully.
	BatchSucceeded BatchPhase = "Succeeded"
	// BatchFailed means that the Batch has failed.
	BatchFailed BatchPhase = "Failed"
)

// BatchStatus defines the observed state of Batch
type BatchStatus struct {
	// Current phase of the batch operation
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Phase"
	Phase BatchPhase `json:"phase"`
	// The reason for any batch related failures
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Reason"
	Reason string `json:"reason,omitempty"`
	// The UUID of the Infinispan instance that the Batch is associated with
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Cluster UUID"
	ClusterUID *types.UID `json:"clusterUID,omitempty"`
}

// +kubebuilder:object:root=true

// +kubebuilder:subresource:status
// +kubebuilder:resource:path=batches,scope=Namespaced
// Batch is the Schema for the batches API
type Batch struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BatchSpec   `json:"spec,omitempty"`
	Status BatchStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// BatchList contains a list of Batch
type BatchList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Batch `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Batch{}, &BatchList{})
}
