package v2alpha1

// IMPORTANT: run "make codegen" or "operator-sdk generate k8s" to regenerate code after modifying this file
// NOTE: json tags are required. Any new fields you add must have json tags for the fields to be serialized.

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// AdminAuth description of the auth info
type AdminAuth struct {
	// name of the secret containing both admin username and password
	SecretName string `json:"secretName,optional,omitempty"`
	// Secret and key containing the admin username for authentication.
	Username v1.SecretKeySelector `json:"username,optional,omitempty"`
	// Secret and key containing the admin password for authentication.
	Password v1.SecretKeySelector `json:"password,optional,omitempty"`
}

// CacheSpec defines the desired state of Cache
type CacheSpec struct {
	// Deprecated. This no longer has any effect. The operator's admin credentials are now used to perform cache operations
	AdminAuth *AdminAuth `json:"adminAuth,omitempty"`
	// Name of the cluster where to create the cache
	ClusterName string `json:"clusterName,omitempty"`
	// Name of the cache to be created. If empty ObjectMeta.Name will be used
	Name string `json:"name,optional,omitempty"`
	// Cache template in XML format
	Template string `json:"template,optional,omitempty"`
	// Name of the template to be used to create this cache
	TemplateName string `json:"templateName,optional,omitempty"`
}

// CacheCondition define a condition of the cluster
type CacheCondition struct {
	// Type is the type of the condition.
	Type string `json:"type"`
	// Status is the status of the condition.
	Status metav1.ConditionStatus `json:"status"`
	// Human-readable message indicating details about last transition.
	Message string `json:"message,optional,omitempty"`
}

// CacheStatus defines the observed state of Cache
type CacheStatus struct {
	// Conditions list for this cache
	Conditions []CacheCondition `json:"conditions,optional,omitempty"`
	// Service name that exposes the cache inside the cluster
	ServiceName string `json:"serviceName,optional,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Cache is the Schema for the caches API
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
