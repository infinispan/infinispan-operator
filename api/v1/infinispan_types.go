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
	// A secret that contains CredentialStore alias and password combinations
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="CredentialStore Secret",xDescriptors={"urn:alm:descriptor:io.kubernetes:Secret"}
	CredentialStoreSecretName string `json:"credentialStoreSecretName,omitempty"`
	// Enable or disable user authentication
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Toggle Authentication",xDescriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	EndpointAuthentication *bool `json:"endpointAuthentication,omitempty"`
	// The secret that contains user credentials.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Authentication Secret",xDescriptors={"urn:alm:descriptor:io.kubernetes:Secret", "urn:alm:descriptor:com.tectonic.ui:fieldDependency:security.endpointAuthentication:true"}
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
	// Disable or modify endpoint encryption.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Configure Encryption",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:Service","urn:alm:descriptor:com.tectonic.ui:select:Secret","urn:alm:descriptor:com.tectonic.ui:select:None"}
	Type CertificateSourceType `json:"type,omitempty"`
	// A service that provides TLS certificates
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Encryption Service",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text", "urn:alm:descriptor:com.tectonic.ui:fieldDependency:security.endpointEncryption.type:Service"}
	CertServiceName string `json:"certServiceName,omitempty"`
	// The secret that contains TLS certificates
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Encryption Secret",xDescriptors={"urn:alm:descriptor:io.kubernetes:Secret", "urn:alm:descriptor:com.tectonic.ui:fieldDependency:security.endpointEncryption.type:Secret"}
	CertSecretName string `json:"certSecretName,omitempty"`
	// +optional
	ClientCert ClientCertType `json:"clientCert,omitempty"`
	// +optional
	ClientCertSecretName string `json:"clientCertSecretName,omitempty"`
}

// InfinispanServiceContainerSpec resource requirements specific for service
type InfinispanServiceContainerSpec struct {
	// The amount of storage for the persistent volume claim.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Storage Size",xDescriptors="urn:alm:descriptor:text"
	Storage *string `json:"storage,omitempty"`
	// Enable/disable container ephemeral storage
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Container Ephemeral Storage",xDescriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	EphemeralStorage bool `json:"ephemeralStorage,omitempty"`
	// The storage class object for persistent volume claims
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Storage Class Name",xDescriptors={"urn:alm:descriptor:io.kubernetes:StorageClass", "urn:alm:descriptor:com.tectonic.ui:fieldDependency:service.container.ephemeralStorage:false"}
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
	// The service type
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Service Type",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:Cache", "urn:alm:descriptor:com.tectonic.ui:select:DataGrid"}
	Type ServiceType `json:"type,omitempty"`
	// +optional
	Container *InfinispanServiceContainerSpec `json:"container,omitempty"`
	// +optional
	Sites *InfinispanSitesSpec `json:"sites,omitempty"`
	// Cache replication factor, or number of copies for each entry.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Number of Owners",xDescriptors="urn:alm:descriptor:com.tectonic.ui:number"
	ReplicationFactor int32 `json:"replicationFactor,omitempty"`
}

// InfinispanContainerSpec specify resource requirements per container
type InfinispanContainerSpec struct {
	// +optional
	CliExtraJvmOpts string `json:"cliExtraJvmOpts,omitempty"`
	// +optional
	ExtraJvmOpts string `json:"extraJvmOpts,omitempty"`
	// +optional
	RouterExtraJvmOpts string `json:"routerExtraJvmOpts,omitempty"`
	// +optional
	Memory string `json:"memory,omitempty"`
	// +optional
	CPU string `json:"cpu,omitempty"`
}

// InfinispanSitesLocalSpec enables cross-site replication
type InfinispanSitesLocalSpec struct {
	Name   string              `json:"name"`
	Expose CrossSiteExposeSpec `json:"expose"`
	// +optional
	MaxRelayNodes int32 `json:"maxRelayNodes,omitempty"`
	// +optional
	Encryption *EncryptionSiteSpec `json:"encryption,omitempty"`
	// +optional
	Discovery *DiscoverySiteSpec `json:"discovery,omitempty"`
}

// DiscoverySiteSpec configures the corss-site replication discovery
type DiscoverySiteSpec struct {
	// Configures the discovery mode for cross-site replication
	// +optional
	Type DiscoverySiteType `json:"type,omitempty"`
	// Enables (default) or disables the Gossip Router pod and cross-site services
	// +optional
	LaunchGossipRouter *bool `json:"launchGossipRouter,omitempty"`
}

// Specifies the discovery mode for cross-site configuration
// +kubebuilder:validation:Enum=gossiprouter
type DiscoverySiteType string

// GossipRouterType is the only one supported but we may add other like submariner and/or skupper
const (
	GossipRouterType DiscoverySiteType = "gossiprouter"
)

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
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Node Port",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number", "urn:alm:descriptor:com.tectonic.ui:fieldDependency:service.sites.local.expose.type:NodePort"}
	Port *int32 `json:"port,omitempty"`
	// +kubebuilder:validation:Pattern=`(^(kubernetes|minikube|openshift):\/\/(([a-z0-9]|[a-z0-9][a-z0-9\-]*[a-z0-9])\.)*([a-z0-9]|[a-z0-9][a-z0-9\-]*[a-z0-9])*(:[0-9]+)+$)|(^(infinispan\+xsite):\/\/(([a-z0-9]|[a-z0-9][a-z0-9\-]*[a-z0-9])\.)*([a-z0-9]|[a-z0-9][a-z0-9\-]*[a-z0-9])*(:[0-9]+)*$)`
	// +optional
	URL string `json:"url,omitempty"`
	// The access secret that allows backups to a remote site
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Backup Location Secret",xDescriptors="urn:alm:descriptor:io.kubernetes:Secret"
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
// +kubebuilder:validation:Enum=NodePort;LoadBalancer;ClusterIP;Route
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

	// CrossSiteExposeTypeRoute route
	CrossSiteExposeTypeRoute = "Route"
)

// CrossSiteSchemeType specifies the supported url scheme's allowed in InfinispanSiteLocationSpec.URL
type CrossSiteSchemeType string

const (
	CrossSiteSchemeTypeKubernetes = "kubernetes"
	CrossSiteSchemeTypeMinikube   = "minikube"
	CrossSiteSchemeTypeOpenShift  = "openshift"
)

// ExposeSpec describe how Infinispan will be exposed externally
type ExposeSpec struct {
	// Type specifies different exposition methods for data grid
	Type ExposeType `json:"type"`
	// +optional
	NodePort int32 `json:"nodePort,omitempty"`
	// +optional
	Port int32 `json:"port,omitempty"`
	// The network hostname for your Infinispan cluster
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Route Hostname",xDescriptors={"urn:alm:descriptor:com.tectonic.ui:text", "urn:alm:descriptor:com.tectonic.ui:fieldDependency:expose.type:Route"}
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
	// RouteHostName optionally, specifies a custom hostname to be used by Openshift Route.
	// +optional
	RouteHostName string `json:"routeHostName,omitempty"`
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
	// The Persistent Volume Claim that holds custom libraries
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Persistent Volume Claim Name",xDescriptors="urn:alm:descriptor:io.kubernetes:PersistentVolumeClaim"
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
	// +optional
	// +kubebuilder:validation:Pattern=`^$|^(https?|ftp)://[-a-zA-Z0-9+&@#/%?=~_|!:,.;]*[-a-zA-Z0-9+&@#/%=~_|]`
	// URL of the file you want to download.
	Url string `json:"url"`
	// +optional
	// +kubebuilder:validation:Pattern=`^$|^([-_a-zA-Z0-9.]+):([-_a-zA-Z0-9.]+):([-_a-zA-Z0-9.]+)(?::([-_a-zA-Z0-9.]+))?$`
	// Coordinates of a maven artifact in the `groupId:artifactId:version` format, for example `org.postgresql:postgresql:42.3.1`.
	Maven string `json:"maven"`
	// +optional
	// Deprecated, no longer has any effect. Specifies the type of file you want to download. If not specified, the file type is automatically determined from the extension.
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

type ConfigListenerSpec struct {
	// If true, a dedicated pod is used to ensure that all config resources created on the Infinispan server have a matching CR resource
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Toggle Config Listener",xDescriptors="urn:alm:descriptor:com.tectonic.ui:booleanSwitch"
	Enabled bool `json:"enabled"`
}

// InfinispanSpec defines the desired state of Infinispan
type InfinispanSpec struct {
	// The number of nodes in the Infinispan cluster.
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:podCount"
	Replicas int32 `json:"replicas"`
	// The semantic version of the Infinispan cluster.
	// +optional
	// +kubebuilder:validation:Pattern=`^$|^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`
	Version string `json:"version"`
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
	// +optional
	ConfigMapName string `json:"configMapName,omitempty"`
	// Strategy to use when doing upgrades
	Upgrades *InfinispanUpgradesSpec `json:"upgrades,omitempty"`
	// +optional
	ConfigListener *ConfigListenerSpec `json:"configListener,omitempty"`
}

// InfinispanUpgradesSpec defines the Infinispan upgrade strategy
type InfinispanUpgradesSpec struct {
	Type UpgradeType `json:"type"`
}

type UpgradeType string

const (
	// UpgradeTypeHotRodRolling Upgrade with no downtime and data copied over Hot Rod
	UpgradeTypeHotRodRolling UpgradeType = "HotRodRolling"
	// UpgradeTypeShutdown Upgrade requires downtime and data persisted in cache stores
	UpgradeTypeShutdown UpgradeType = "Shutdown"
)

type ConditionType string

const (
	ConditionPrelimChecksPassed  ConditionType = "PreliminaryChecksPassed"
	ConditionGracefulShutdown    ConditionType = "GracefulShutdown"
	ConditionStopping            ConditionType = "Stopping"
	ConditionUpgrade             ConditionType = "Upgrade"
	ConditionWellFormed          ConditionType = "WellFormed"
	ConditionCrossSiteViewFormed ConditionType = "CrossSiteViewFormed"
	ConditionGossipRouterReady   ConditionType = "GossipRouterReady"
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
	// The Pod's currently in the cluster
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Pod Status",xDescriptors="urn:alm:descriptor:com.tectonic.ui:podStatuses"
	PodStatus DeploymentStatus `json:"podStatus,omitempty"`
	// Infinispan Console URL
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Infinispan Console URL",xDescriptors="urn:alm:descriptor:org.w3:link"
	ConsoleUrl *string `json:"consoleUrl,omitempty"`
	// +optional
	HotRodRollingUpgradeStatus *HotRodRollingUpgradeStatus `json:"hotRodRollingUpgradeStatus,omitempty"`
	// The Operand status
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=status,displayName="Operand Status"
	Operand OperandStatus `json:"operand,omitempty"`
}

type OperandPhase string

const (
	// OperandPhasePending indicates that the configured Operand is currently being provisioned and the cluster is not WellFormed
	OperandPhasePending OperandPhase = "Pending"
	// OperandPhaseRunning indicates that the Operand has been provisioned and is WellFormed
	OperandPhaseRunning OperandPhase = "Running"
)

type OperandStatus struct {
	// The Image being used by the Operand currently being reconciled
	// +optional
	Image string `json:"image,omitempty"`
	// The most recently observed Phase of the Operand deployment
	// +optional
	Phase OperandPhase `json:"phase,omitempty"`
	// The Operand version to be reconciled
	// +optional
	Version string `json:"version,omitempty"`
}

type HotRodRollingUpgradeStatus struct {
	Stage                 HotRodRollingUpgradeStage `json:"stage,omitempty"`
	SourceStatefulSetName string                    `json:"SourceStatefulSetName,omitempty"`
	SourceVersion         string                    `json:"SourceVersion,omitempty"`
	TargetStatefulSetName string                    `json:"TargetStatefulSetName,omitempty"`
}

type HotRodRollingUpgradeStage string

const (
	HotRodRollingStageStart              HotRodRollingUpgradeStage = "HotRodRollingStageStart"
	HotRodRollingStagePrepare            HotRodRollingUpgradeStage = "HotRodRollingStagePrepare"
	HotRodRollingStageRedirect           HotRodRollingUpgradeStage = "HotRodRollingStageRedirect"
	HotRodRollingStageSync               HotRodRollingUpgradeStage = "HotRodRollingStageSync"
	HotRodRollingStageStatefulSetReplace HotRodRollingUpgradeStage = "HotRodRollingStageStatefulSetReplace"
	HotRodRollingStageCleanup            HotRodRollingUpgradeStage = "HotRodRollingStageCleanup"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +operator-sdk:csv:customresourcedefinitions:displayName="Infinispan Cluster"

// Infinispan is the Schema for the infinispans API
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

// TLSProtocol specifies the TLS protocol
// +kubebuilder:validation:Enum=TLSv1.2;TLSv1.3
type TLSProtocol string

// Note: TLS v1.1 and older are not consider secure anymore
const (
	TLSVersion12 TLSProtocol = "TLSv1.2"
	TLSVersion13 TLSProtocol = "TLSv1.3"
)

// EncryptionSiteSpec enables TLS for cross-site replication
type EncryptionSiteSpec struct {
	// +optional
	Protocol          TLSProtocol       `json:"protocol,omitempty"`
	TransportKeyStore CrossSiteKeyStore `json:"transportKeyStore"`
	RouterKeyStore    CrossSiteKeyStore `json:"routerKeyStore"`
	// +optional
	TrustStore *CrossSiteTrustStore `json:"trustStore,omitempty"`
}

// CrossSiteKeyStore keystore configuration for cross-site replication with TLS
type CrossSiteKeyStore struct {
	SecretName string `json:"secretName"`
	// +optional
	Alias string `json:"alias,omitempty"`
	// +optional
	Filename string `json:"filename,omitempty"`
}

// CrossSiteTrustStore truststore configuration for cross-site replication with TLS
type CrossSiteTrustStore struct {
	SecretName string `json:"secretName"`
	// +optional
	Filename string `json:"filename,omitempty"`
}

func init() {
	SchemeBuilder.Register(&Infinispan{}, &InfinispanList{})
}
