package v2alpha1

// IMPORTANT: run "make codegen" or "operator-sdk generate k8s" to regenerate code after modifying this file
// NOTE: json tags are required. Any new fields you add must have json tags for the fields to be serialized.

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type SchemaConditionType string

const (
	SchemaConditionReady             SchemaConditionType = "Ready"
	SchemaConditionBidirectionalSync SchemaConditionType = "BidirectionalSync"
)

// SchemaSpec defines the desired state of Schema
type SchemaSpec struct {
	// Infinispan cluster name
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Cluster Name",xDescriptors="urn:alm:descriptor:io.kubernetes:infinispan.org:v1:Infinispan"
	ClusterName string `json:"clusterName"`
	// Name of the schema on the Infinispan server. If empty ObjectMeta.Name will be used. The .proto suffix is added automatically if not present
	// +optional
	Name string `json:"name,omitempty"`
	// Protobuf schema definition
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Schema Definition"
	Schema string `json:"schema"`
}

// SchemaCondition define a condition of the schema
type SchemaCondition struct {
	// Type is the type of the condition.
	Type SchemaConditionType `json:"type"`
	// Status is the status of the condition.
	Status metav1.ConditionStatus `json:"status"`
	// Human-readable message indicating details about last transition.
	// +optional
	Message string `json:"message,omitempty"`
}

// SchemaStatus defines the observed state of Schema
type SchemaStatus struct {
	// Conditions list for this schema
	// +optional
	Conditions []SchemaCondition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true

// Schema is the Schema for the schemas API
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=schemas,scope=Namespaced
type Schema struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SchemaSpec   `json:"spec,omitempty"`
	Status SchemaStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// SchemaList contains a list of Schema
type SchemaList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Schema `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Schema{}, &SchemaList{})
}
