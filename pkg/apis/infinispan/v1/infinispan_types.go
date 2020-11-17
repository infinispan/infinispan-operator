package v1

// IMPORTANT: run "make codegen" or "operator-sdk generate k8s" to regenerate code after modifying this file
// NOTE: json tags are required. Any new fields you add must have json tags for the fields to be serialized.

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// InfinispanAuthInfo authentication info
type InfinispanAuthInfo struct {
	Type string `json:"type"`
}

// InfinispanSecurity info for the user application connection
type InfinispanSecurity struct {
	EndpointSecretName string              `json:"endpointSecretName,optional,omitempty"`
	EndpointEncryption *EndpointEncryption `json:"endpointEncryption,optional,omitempty"`
}

// CertificateSourceType specifies all the possible sources for the encryption certificate
// +kubebuilder:validation:Enum=Service;service;Secret;secret
type CertificateSourceType string

const (
	// CertificateSourceTypeService certificate coming from a cluster service
	CertificateSourceTypeService CertificateSourceType = "Service"
	// CertificateSourceTypeServiceLowCase certificate coming from a cluster service
	CertificateSourceTypeServiceLowCase CertificateSourceType = "service"

	// CertificateSourceTypeSecret certificate coming from a user provided secret
	CertificateSourceTypeSecret CertificateSourceType = "Secret"
	// CertificateSourceTypeSecretLowCase certificate coming from a user provided secret
	CertificateSourceTypeSecretLowCase CertificateSourceType = "secret"
)

// EndpointEncryption configuration
type EndpointEncryption struct {
	Type            CertificateSourceType `json:"type,optional,omitempty"`
	CertServiceName string                `json:"certServiceName,optional,omitempty"`
	CertSecretName  string                `json:"certSecretName,optional,omitempty"`
}

// InfinispanServiceContainerSpec resource requirements specific for service
type InfinispanServiceContainerSpec struct {
	Storage          *string `json:"storage,optional,omitempty"`
	EphemeralStorage *bool   `json:"ephemeralStorage,optional,omitempty"`
	StorageClassName *string `json:"storageClassName,optional,omitempty"`
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
	Type              ServiceType                     `json:"type"`
	Container         *InfinispanServiceContainerSpec `json:"container,optional,omitempty"`
	Sites             *InfinispanSitesSpec            `json:"sites,optional,omitempty"`
	ReplicationFactor int32                           `json:"replicationFactor,optional,omitempty"`
}

// InfinispanContainerSpec specify resource requirements per container
type InfinispanContainerSpec struct {
	ExtraJvmOpts string `json:"extraJvmOpts,optional,omitempty"`
	Memory       string `json:"memory,optional,omitempty"`
	CPU          string `json:"cpu,optional,omitempty"`
}

type InfinispanSitesLocalSpec struct {
	Name   string     `json:"name"`
	Expose ExposeSpec `json:"expose"`
}

type InfinispanSiteLocationSpec struct {
	Name       string `json:"name"`
	Host       string `json:"host,optional,omitempty"`
	Port       int32  `json:"port,optional,omitempty"`
	URL        string `json:"url,optional,omitempty"`
	SecretName string `json:"secretName,optional,omitempty"`
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
	// Type specifies different exposition methods for data grid
	Type        ExposeType        `json:"type"`
	NodePort    int32             `json:"nodePort,optional,omitempty"`
	Host        string            `json:"host,optional,omitempty"`
	Annotations map[string]string `json:"annotations,optional,omitempty"`
}

// Autoscale describe autoscaling configuration for the cluster
type Autoscale struct {
	MaxReplicas        int32 `json:"maxReplicas"`
	MinReplicas        int32 `json:"minReplicas"`
	MaxMemUsagePercent int   `json:"maxMemUsagePercent"`
	MinMemUsagePercent int   `json:"minMemUsagePercent"`
	Disabled           bool  `json:"disabled,optional,omitempty"`
}

// InfinispanSpec defines the desired state of Infinispan
type InfinispanSpec struct {
	Replicas  int32                   `json:"replicas"`
	Image     *string                 `json:"image,optional,omitempty"`
	Security  InfinispanSecurity      `json:"security,optional,omitempty"`
	Container InfinispanContainerSpec `json:"container,optional,omitempty"`
	Service   InfinispanServiceSpec   `json:"service,optional,omitempty"`
	Logging   *InfinispanLoggingSpec  `json:"logging,optional,omitempty"`
	Expose    *ExposeSpec             `json:"expose,optional,omitempty"`
	Autoscale *Autoscale              `json:"autoscale,optional,omitempty"`
	Affinity  *corev1.Affinity        `json:"affinity,optional,omitempty"`
}

type ConditionType string

const (
	ConditionPrelimChecksPassed ConditionType = "PreliminaryChecksPassed"
	ConditionGracefulShutdown   ConditionType = "GracefulShutdown"
	ConditionStopping           ConditionType = "Stopping"
	ConditionUpgrade            ConditionType = "Upgrade"
	ConditionWellFormed         ConditionType = "WellFormed"
)

// InfinispanCondition define a condition of the cluster
type InfinispanCondition struct {
	// Type is the type of the condition.
	Type ConditionType `json:"type"`
	// Status is the status of the condition.
	Status metav1.ConditionStatus `json:"status"`
	// Human-readable message indicating details about last transition.
	Message string `json:"message,optional,omitempty"`
}

// InfinispanStatus defines the observed state of Infinispan
type InfinispanStatus struct {
	// +optional
	Conditions              []InfinispanCondition `json:"conditions,omitempty"`
	StatefulSetName         string                `json:"statefulSetName,optional,omitempty"`
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
