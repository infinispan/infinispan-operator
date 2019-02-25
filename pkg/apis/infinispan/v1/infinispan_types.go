package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// InfinispanSpec defines the desired state of Infinispan
type InfinispanSpec struct {
	Size  int32  `json:"size"`
	Image string `json:"image"`
}

// InfinispanStatus defines the observed state of Infinispan
type InfinispanStatus struct {
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	Nodes []string `json:"nodes"`
}

type Config struct {
	Name string `json:"name"`
}

type ConfigMapConfig struct {
	Name string `json:"name"`
}

type ConfigSource string

const (
	// Internal means a configuration already shipped with Infinispan
	Internal ConfigSource = "Internal"

	ConfigMap ConfigSource = "ConfigMap"
)

// InfinispanConfig defines the profile (configuration) to start the cluster
type InfinispanConfig struct {
	// The path to the xml of the config to use, e.g. "cloud.xml". This must be relative to "/opt/jboss/infinispan-server/standalone/configuration"
	Name string `json:"name,omitempty"`

	// Where to find the config
	SourceType ConfigSource `json:"sourceType,omitempty"`

	// If the source is external, the reference to the resource
	SourceRef string `json:"sourceRef,omitempty"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Infinispan is the Schema for the infinispans API
// +k8s:openapi-gen=true
type Infinispan struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Config InfinispanConfig `json:"config,omitempty"`
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
