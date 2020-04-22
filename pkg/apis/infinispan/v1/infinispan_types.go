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
	EndpointSecretName string             `json:"endpointSecretName,optional,omitempty"`
	EndpointEncryption EndpointEncryption `json:"endpointEncryption,optional,omitempty"`
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

type ServiceType string

const (
	// Deploys Infinispan to act like a cache. This means:
	// Caches are only used for volatile data.
	// No support for data persistence.
	// Cache definitions can still be permanent, but PV size is not configurable.
	// A default cache is created by default,
	// Additional caches can be created, but only as copies of default cache.
	ServiceTypeCache ServiceType = "Cache"

	// Deploys Infinispan to act like a data grid.
	// More flexibility and more configuration options are available:
	// Cross-site replication, store cached data in persistence store...etc.
	ServiceTypeDataGrid ServiceType = "DataGrid"
)

// InfinispanServiceSpec specify configuration for specific service
type InfinispanServiceSpec struct {
	Type              ServiceType                    `json:"type"`
	Container         InfinispanServiceContainerSpec `json:"container"`
	Sites             InfinispanSitesSpec            `json:"sites,optional,omitempty"`
	ReplicationFactor int32                          `json:"replicationFactor,optional,omitempty"`
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

type InfinispanSiteLocationSpec struct {
	Name       string `json:"name"`
	URL        string `json:"url"`
	SecretName string `json:"secretName"`
}

type InfinispanSitesSpec struct {
	Local     InfinispanSitesLocalSpec     `json:"local"`
	Locations []InfinispanSiteLocationSpec `json:"locations,omitempty"`
}

type InfinispanLoggingSpec struct {
	Categories map[string]string `json:"categories,omitempty"`
}

// ExposeType describe different exposition methods for Infinispan
// +kubebuilder:validation:Enum=NodePort;LoadBalancer;Route
type ExposeType string

const (
	// ExposeTypeNodePort means a service will be exposed on one port of
	// every node, in addition to 'ClusterIP' type.
	ExposeTypeNodePort ExposeType = "NodePort"

	// ExposeTypeLoadBalancer means a service will be exposed via an
	// external load balancer (if the cloud provider supports it), in addition
	// to 'NodePort' type.
	ExposeTypeLoadBalancer ExposeType = "LoadBalancer"

	// ExposeTypeRoute means the service will be exposed via
	// `Route` on Openshift or via `Ingress` on Kubernetes
	ExposeTypeRoute ExposeType = "Route"
)

// ExposeSpec describe how Infinispan will be exposed externally
type ExposeSpec struct {
	// Type specifies different exposition methods for datagrid
	Type     ExposeType `json:"type"`
	NodePort int32      `json:"nodePort,optional,omitempty"`
	Host     string     `json:"host,optional,omitempty"`
}

// Autoscale describe autoscaling configuration for the cluster
type Autoscale struct {
	MaxReplicas        int32 `json:"maxReplicas"`
	MinReplicas        int32 `json:"minReplicas"`
	MaxMemUsagePercent int   `json:"maxMemUsagePercent"`
	MinMemUsagePercent int   `json:"minMemUsagePercent"`
}

// InfinispanSpec defines the desired state of Infinispan
type InfinispanSpec struct {
	Replicas  int32                   `json:"replicas"`
	Image     string                  `json:"image,optional,omitempty"`
	Security  InfinispanSecurity      `json:"security,optional,omitempty"`
	Container InfinispanContainerSpec `json:"container,optional,omitempty"`
	Service   InfinispanServiceSpec   `json:"service,optional,omitempty"`
	Logging   InfinispanLoggingSpec   `json:"logging,optional,omitempty"`
	Expose    *ExposeSpec             `json:"expose,optional,omitempty"`
	Autoscale *Autoscale              `json:"autoscale,optional,omitempty"`
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
	// +optional
	Conditions              []InfinispanCondition `json:"conditions,omitempty"`
	StatefulSetName         string                `json:"statefulSetName"`
	Security                InfinispanSecurity    `json:"security"`
	ReplicasWantedAtRestart int32                 `json:"replicasWantedAtRestart,omitempty"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Infinispan is the Schema for the infinispans API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
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
