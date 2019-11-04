package v1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// InfinispanAuthInfo authentication info
type InfinispanAuthInfo struct {
	Type string `json:"type"`
}

// InfinispanSecurity info for the user application connection
type InfinispanSecurity struct {
	EndpointSecretName string             `json:"endpointSecretName"`
	EndpointEncryption EndpointEncryption `json:"endpointEncryption"`
}

// EndpointEncryption configuration
type EndpointEncryption struct {
	Type            string `json:"type"`
	CertServiceName string `json:"certServiceName"`
	CertSecretName  string `json:"certSecretName"`
}

// InfinispanServiceContainerSpec resource requirements specific for service
type InfinispanServiceContainerSpec struct {
	Storage string `json:"storage"`
}

// InfinispanServiceSpec specify configuration for specific service
type InfinispanServiceSpec struct {
	Type      string                         `json:"type"`
	Container InfinispanServiceContainerSpec `json:"container"`
	Sites     InfinispanSitesSpec            `json:"sites"`
}

// InfinispanContainerSpec specify resource requirements per container
type InfinispanContainerSpec struct {
	ExtraJvmOpts string `json:"extraJvmOpts"`
	Memory       string `json:"memory"`
	CPU          string `json:"cpu"`
}

type InfinispanSitesLocalSpec struct {
	Name   string         `json:"name"`
	Expose v1.ServiceSpec `json:"expose"`
}

type InfinispanSitesBackupSpec struct {
	Name       string `json:"name"`
	URL        string `json:"url"`
	SecretName string `json:"secretName"`
}

type InfinispanSitesSpec struct {
	Local   InfinispanSitesLocalSpec    `json:"local"`
	Backups []InfinispanSitesBackupSpec `json:"backups"`
}

type InfinispanLoggingSpec struct {
	Categories map[string]string `json:"categories"`
}

// InfinispanSpec defines the desired state of Infinispan
type InfinispanSpec struct {
	Replicas  int32                   `json:"replicas"`
	Image     string                  `json:"image"`
	Security  InfinispanSecurity      `json:"security"`
	Container InfinispanContainerSpec `json:"container"`
	Service   InfinispanServiceSpec   `json:"service"`
	Logging   InfinispanLoggingSpec   `json:"logging"`
	Expose    v1.ServiceSpec          `json:"expose"`
}

// InfinispanCondition define a condition of the cluster
type InfinispanCondition struct {
	// Type is the type of the condition.
	Type string `json:"type"`
	// Status is the status of the condition.
	Status string `json:"status"`
	// Human-readable message indicating details about last transition.
	Message string `json:"message"`
}

// InfinispanStatus defines the observed state of Infinispan
type InfinispanStatus struct {
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	Conditions      []InfinispanCondition `json:"conditions"`
	StatefulSetName string                `json:"statefulSetName"`
	Security        InfinispanSecurity    `json:"security"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Infinispan is the Schema for the infinispans API
// +k8s:openapi-gen=true
type Infinispan struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   InfinispanSpec   `json:"spec,omitempty"`
	Status InfinispanStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// InfinispanList contains a list of Infinispan
type InfinispanList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Infinispan `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Infinispan{}, &InfinispanList{})
}
