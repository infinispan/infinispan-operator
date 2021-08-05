package v1

// IMPORTANT: run "make codegen" or "operator-sdk generate k8s" to regenerate code after modifying this file
// NOTE: json tags are required. Any new fields you add must have json tags for the fields to be serialized.

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// InfinispanSecurity info for the user application connection
type InfinispanSecurity struct {
	// +optional
	Authorization *Authorization `json:"authorization,omitempty"`
	// +optional
	EndpointAuthentication *bool `json:"endpointAuthentication,omitempty"`
	// +optional
	EndpointSecretName string `json:"endpointSecretName,omitempty"`
	// +optional
	EndpointEncryption *EndpointEncryption `json:"endpointEncryption,omitempty"`
}

type Authorization struct {
	// +optional
	Enabled bool `json:"enabled,omitempty"`
	// +optional
	Roles []AuthorizationRole `json:"roles,omitempty"`
}

type AuthorizationRole struct {
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

// CertificateSourceType specifies all the possible sources for the encryption certificate
// +kubebuilder:validation:Enum=Service;service;Secret;secret;None
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

	// CertificateSourceTypeNoneNoEncryption no certificate encryption disabled
	CertificateSourceTypeNoneNoEncryption CertificateSourceType = "None"
)

// ClientCertType specifies a client certificate validation mechanism.
// +kubebuilder:validation:Enum=None;Authenticate;Validate
type ClientCertType string

const (
	// No client certificates required
	ClientCertNone ClientCertType = "None"
	// All client certificates must be in the configured truststore.
	ClientCertAuthenticate ClientCertType = "Authenticate"
	// Client certificates are validated against the CA in the truststore. It is not required for all client certificates to be contained in the trustore.
	ClientCertValidate ClientCertType = "Validate"
)

// EndpointEncryption configuration
type EndpointEncryption struct {
	// +optional
	Type CertificateSourceType `json:"type,omitempty"`
	// +optional
	CertServiceName string `json:"certServiceName,omitempty"`
	// +optional
	CertSecretName string `json:"certSecretName,omitempty"`
	// +optional
	ClientCert ClientCertType `json:"clientCert,omitempty"`
	// +optional
	ClientCertSecretName string `json:"clientCertSecretName,omitempty"`
}

// InfinispanServiceContainerSpec resource requirements specific for service
type InfinispanServiceContainerSpec struct {
	// +optional
	Storage *string `json:"storage,omitempty"`
	// +optional
	EphemeralStorage bool `json:"ephemeralStorage,omitempty"`
	// +optional
	StorageClassName string `json:"storageClassName,omitempty"`
}

// +kubebuilder:validation:Enum=DataGrid;Cache
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
	Type ServiceType `json:"type,omitempty"`
	// +optional
	Container *InfinispanServiceContainerSpec `json:"container,omitempty"`
	// +optional
	Sites *InfinispanSitesSpec `json:"sites,omitempty"`
	// +optional
	ReplicationFactor int32 `json:"replicationFactor,omitempty"`
}

// InfinispanContainerSpec specify resource requirements per container
type InfinispanContainerSpec struct {
	// +optional
	ExtraJvmOpts string `json:"extraJvmOpts,omitempty"`
	// +optional
	Memory string `json:"memory,omitempty"`
	// +optional
	CPU string `json:"cpu,omitempty"`
}

type InfinispanSitesLocalSpec struct {
	Name   string              `json:"name"`
	Expose CrossSiteExposeSpec `json:"expose"`
}

type InfinispanSiteLocationSpec struct {
	Name string `json:"name"`
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// +optional
	ClusterName string `json:"clusterName,omitempty"`
	// Deprecated and to be removed on subsequent release. Use .URL with infinispan+xsite schema instead.
	// +optional
	Host *string `json:"host,omitempty"`
	// Deprecated and to be removed on subsequent release. Use .URL with infinispan+xsite schema instead.
	// +optional
	Port *int32 `json:"port,omitempty"`
	// +kubebuilder:validation:Pattern=`(^(kubernetes|minikube|openshift):\/\/(([a-z0-9]|[a-z0-9][a-z0-9\-]*[a-z0-9])\.)*([a-z0-9]|[a-z0-9][a-z0-9\-]*[a-z0-9])*(:[0-9]+)+$)|(^(infinispan\+xsite):\/\/(([a-z0-9]|[a-z0-9][a-z0-9\-]*[a-z0-9])\.)*([a-z0-9]|[a-z0-9][a-z0-9\-]*[a-z0-9])*(:[0-9]+)*$)`
	// +optional
	URL string `json:"url,omitempty"`
	// +optional
	SecretName string `json:"secretName,omitempty"`
}

type InfinispanSitesSpec struct {
	Local     InfinispanSitesLocalSpec     `json:"local"`
	Locations []InfinispanSiteLocationSpec `json:"locations,omitempty"`
}

// LoggingLevelType describe the logging level for selected category
// +kubebuilder:validation:Enum=trace;debug;info;warn;error
type LoggingLevelType string

const (
	LoggingLevelTrace LoggingLevelType = "trace"
	LoggingLevelDebug LoggingLevelType = "debug"
	LoggingLevelInfo  LoggingLevelType = "info"
	LoggingLevelWarn  LoggingLevelType = "warn"
	LoggingLevelError LoggingLevelType = "error"
)

type InfinispanLoggingSpec struct {
	Categories map[string]LoggingLevelType `json:"categories,omitempty"`
}

// ExposeType describe different exposition methods for Infinispan
// +kubebuilder:validation:Enum=NodePort;LoadBalancer;Route
type ExposeType string

const (
	// ExposeTypeNodePort means a service will be exposed on one port of
	// every node, in addition to 'ClusterIP' type.
	ExposeTypeNodePort = ExposeType(corev1.ServiceTypeNodePort)

	// ExposeTypeLoadBalancer means a service will be exposed via an
	// external load balancer (if the cloud provider supports it), in addition
	// to 'NodePort' type.
	ExposeTypeLoadBalancer = ExposeType(corev1.ServiceTypeLoadBalancer)

	// ExposeTypeRoute means the service will be exposed via
	// `Route` on Openshift or via `Ingress` on Kubernetes
	ExposeTypeRoute ExposeType = "Route"
)

// CrossSiteExposeType describe different exposition methods for Infinispan Cross-Site service
// +kubebuilder:validation:Enum=NodePort;LoadBalancer;ClusterIP
type CrossSiteExposeType string

const (
	// CrossSiteExposeTypeNodePort means a service will be exposed on one port of
	// every node, in addition to 'ClusterIP' type.
	CrossSiteExposeTypeNodePort = CrossSiteExposeType(corev1.ServiceTypeNodePort)

	// CrossSiteExposeTypeLoadBalancer means a service will be exposed via an
	// external load balancer (if the cloud provider supports it), in addition
	// to 'NodePort' type.
	CrossSiteExposeTypeLoadBalancer = CrossSiteExposeType(corev1.ServiceTypeLoadBalancer)

	// CrossSiteExposeTypeClusterIP means an internal 'ClusterIP'
	// service will be created without external exposition
	CrossSiteExposeTypeClusterIP = CrossSiteExposeType(corev1.ServiceTypeClusterIP)
)

// ExposeSpec describe how Infinispan will be exposed externally
type ExposeSpec struct {
	// Type specifies different exposition methods for data grid
	Type ExposeType `json:"type"`
	// +optional
	NodePort int32 `json:"nodePort,omitempty"`
	// +optional
	Port int32 `json:"port,omitempty"`
	// +optional
	Host string `json:"host,omitempty"`
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// CrossSiteExposeSpec describe how Infinispan Cross-Site service will be exposed externally
type CrossSiteExposeSpec struct {
	// Type specifies different exposition methods for data grid
	Type CrossSiteExposeType `json:"type"`
	// +optional
	NodePort int32 `json:"nodePort,omitempty"`
	// +optional
	Port int32 `json:"port,omitempty"`
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// Autoscale describe autoscaling configuration for the cluster
type Autoscale struct {
	MaxReplicas        int32 `json:"maxReplicas"`
	MinReplicas        int32 `json:"minReplicas"`
	MaxMemUsagePercent int   `json:"maxMemUsagePercent"`
	MinMemUsagePercent int   `json:"minMemUsagePercent"`
	// +optional
	Disabled bool `json:"disabled,omitempty"`
}

// InfinispanExternalDependencies describes all the external dependencies
// used by the Infinispan cluster: i.e. lib folder with custom jar, maven artifact, images ...
type InfinispanExternalDependencies struct {
	// Name of the persistent volume claim with custom libraries
	// +optional
	VolumeClaimName string `json:"volumeClaimName,omitempty"`
	// +optional
	Artifacts []InfinispanExternalArtifacts `json:"artifacts,omitempty"`
}

// ExternalArtifactType defines external artifact file type
// +kubebuilder:validation:Enum=file;zip;tgz
type ExternalArtifactType string

const (
	ExternalArtifactTypeFile  ExternalArtifactType = "file"
	ExternalArtifactTypeZip   ExternalArtifactType = "zip"
	ExternalArtifactTypeTarGz ExternalArtifactType = "tgz"
)

type InfinispanExternalArtifacts struct {
	// +kubebuilder:validation:Pattern=`^(https?|ftp)://[-a-zA-Z0-9+&@#/%?=~_|!:,.;]*[-a-zA-Z0-9+&@#/%=~_|]`
	// URL of the file you want to download.
	Url string `json:"url"`
	// +optional
	// Specifies the type of file you want to download. If not specified, the file type is automatically determined from the extension.
	Type ExternalArtifactType `json:"type,omitempty"`
	// +optional
	// +kubebuilder:validation:Pattern=`^(sha1|sha224|sha256|sha384|sha512|md5):[a-z0-9]+`
	// Checksum that you can use to verify downloaded files.
	Hash string `json:"hash,omitempty"`
}

// InfinispanCloudEvents describes how Infinispan is connected with Cloud Event, see Kafka docs for more info
type InfinispanCloudEvents struct {
	// BootstrapServers is comma separated list of boostrap server:port addresses
	BootstrapServers string `json:"bootstrapServers"`
	// Acks configuration for the producer ack-value
	// +optional
	Acks string `json:"acks,omitempty"`
	// CacheEntriesTopic is the name of the topic on which events will be published
	// +optional
	CacheEntriesTopic string `json:"cacheEntriesTopic,omitempty"`
}

// InfinispanSpec defines the desired state of Infinispan
type InfinispanSpec struct {
	Replicas int32 `json:"replicas"`
	// +optional
	Image *string `json:"image,omitempty"`
	// +optional
	Security InfinispanSecurity `json:"security,omitempty"`
	// +optional
	Container InfinispanContainerSpec `json:"container,omitempty"`
	// +optional
	Service InfinispanServiceSpec `json:"service,omitempty"`
	// +optional
	Logging *InfinispanLoggingSpec `json:"logging,omitempty"`
	// +optional
	Expose *ExposeSpec `json:"expose,omitempty"`
	// +optional
	Autoscale *Autoscale `json:"autoscale,omitempty"`
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
	// +optional
	CloudEvents *InfinispanCloudEvents `json:"cloudEvents,omitempty"`
	// External dependencies needed by the Infinispan cluster
	// +optional
	Dependencies *InfinispanExternalDependencies `json:"dependencies,omitempty"`
}

type ConditionType string

const (
	ConditionPrelimChecksPassed  ConditionType = "PreliminaryChecksPassed"
	ConditionGracefulShutdown    ConditionType = "GracefulShutdown"
	ConditionStopping            ConditionType = "Stopping"
	ConditionUpgrade             ConditionType = "Upgrade"
	ConditionWellFormed          ConditionType = "WellFormed"
	ConditionCrossSiteViewFormed ConditionType = "CrossSiteViewFormed"
)

// InfinispanCondition define a condition of the cluster
type InfinispanCondition struct {
	// Type is the type of the condition.
	Type ConditionType `json:"type"`
	// Status is the status of the condition.
	Status metav1.ConditionStatus `json:"status"`
	// Human-readable message indicating details about last transition.
	// +optional
	Message string `json:"message,omitempty"`
}

type DeploymentStatus struct {
	// Deployments are ready to serve requests
	Ready []string `json:"ready,omitempty"`
	// Deployments are starting, may or may not succeed
	Starting []string `json:"starting,omitempty"`
	// Deployments are not starting, unclear what next step will be
	Stopped []string `json:"stopped,omitempty"`
}

// InfinispanStatus defines the observed state of Infinispan
type InfinispanStatus struct {
	// +optional
	Conditions []InfinispanCondition `json:"conditions,omitempty"`
	// +optional
	StatefulSetName string `json:"statefulSetName,omitempty"`
	// +optional
	Security *InfinispanSecurity `json:"security,omitempty"`
	// +optional
	ReplicasWantedAtRestart int32 `json:"replicasWantedAtRestart,omitempty"`
	// +optional
	PodStatus DeploymentStatus `json:"podStatus,omitempty"`
	// +optional
	ConsoleUrl *string `json:"consoleUrl,omitempty"`
}

// +genclient
// +kubebuilder:object:root=true

// Infinispan is the Schema for the infinispans API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
type Infinispan struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   InfinispanSpec   `json:"spec,omitempty"`
	Status InfinispanStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// InfinispanList contains a list of Infinispan
type InfinispanList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Infinispan `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Infinispan{}, &InfinispanList{})
}
