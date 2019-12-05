package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	NoPhase          CacheConditionType
	PhaseReconciling CacheConditionType = "reconciling"
	PhaseFailing     CacheConditionType = "failing"
)

type CacheConditionType string

// CacheSpec defines the desired state of Cache
// +k8s:openapi-gen=true
type CacheSpec struct {
	// Selector for looking up Infinispan Custom Resources.
	// +kubebuilder:validation:Required
	InfinispanSelector *metav1.LabelSelector `json:"infinispanSelector,omitempty"`
	// A user-provided Cache configuration.
	// +kubebuilder:validation:Required
	CacheConfigurationSpec CacheConfigurationSpec `json:"cacheConfigurationSpec"`
}

type CacheConfigurationSpec interface {

}

// CacheStatus defines the observed state of Cache
// +k8s:openapi-gen=true
type CacheStatus struct {
	// Represents the latest available observations of a statefulset's current state.
	// +optional
	// +patchMergeKey=type
	// +patchStrategy=merge
	Conditions []CacheCondition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,10,rep,name=conditions"`
}

// StatefulSetCondition describes the state of a statefulset at a certain point.
type CacheCondition struct {
	// Type of Cache condition.
	Type CacheConditionType `json:"type" protobuf:"bytes,1,opt,name=type,casttype=DeploymentConditionType"`
	// True if all resources are in a ready state and all work is done.
	Ready bool `json:"ready"`
	// A human readable message indicating details about the transition.
	// +optional
	Message string `json:"message,omitempty" protobuf:"bytes,5,opt,name=message"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Cache is the Schema for the caches API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=caches,scope=Namespaced
type Cache struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CacheSpec   `json:"spec,omitempty"`
	Status CacheStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// CacheList contains a list of Cache
type CacheList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Cache `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Cache{}, &CacheList{})
}
