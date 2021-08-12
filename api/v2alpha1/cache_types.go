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
	// +optional
	SecretName string `json:"secretName,omitempty"`
	// Secret and key containing the admin username for authentication.
	// +optional
	Username v1.SecretKeySelector `json:"username,omitempty"`
	// Secret and key containing the admin password for authentication.
	// +optional
	Password v1.SecretKeySelector `json:"password,omitempty"`
}

// CacheSpec defines the desired state of Cache
type CacheSpec struct {
	// Deprecated. This no longer has any effect. The operator's admin credentials are now used to perform cache operations
	AdminAuth *AdminAuth `json:"adminAuth,omitempty"`
	// Name of the cluster where to create the cache
	ClusterName string `json:"clusterName"`
	// Name of the cache to be created. If empty ObjectMeta.Name will be used
	// +optional
	Name string `json:"name,omitempty"`
	// Cache template in XML format
	// +optional
	Template string `json:"template,omitempty"`
	// Name of the template to be used to create this cache
	// +optional
	TemplateName string `json:"templateName,omitempty"`
}

// CacheCondition define a condition of the cluster
type CacheCondition struct {
	// Type is the type of the condition.
	Type string `json:"type"`
	// Status is the status of the condition.
	Status metav1.ConditionStatus `json:"status"`
	// Human-readable message indicating details about last transition.
	// +optional
	Message string `json:"message,omitempty"`
}

// CacheStatus defines the observed state of Cache
type CacheStatus struct {
	// Conditions list for this cache
	// +optional
	Conditions []CacheCondition `json:"conditions,omitempty"`
	// Service name that exposes the cache inside the cluster
	// +optional
	ServiceName string `json:"serviceName,omitempty"`
}

// +kubebuilder:object:root=true

// Cache is the Schema for the caches API
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=caches,scope=Namespaced
type Cache struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CacheSpec   `json:"spec,omitempty"`
	Status CacheStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
// CacheList contains a list of Cache
type CacheList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Cache `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Cache{}, &CacheList{})
}
