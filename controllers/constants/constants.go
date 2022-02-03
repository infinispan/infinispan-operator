package constants

import (
	"os"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var (
	// DefaultImageName is used if a specific image name is not provided
	DefaultImageName = GetDefaultInfinispanJavaImage()

	// InitContainerImageName allows a custom initContainer image to be used
	InitContainerImageName = GetEnvWithDefault("INITCONTAINER_IMAGE", "registry.access.redhat.com/ubi8-micro")

	// JGroupsDiagnosticsFlag is used to enable traces for JGroups
	JGroupsDiagnosticsFlag = strings.ToUpper(GetEnvWithDefault("JGROUPS_DIAGNOSTICS", "FALSE"))

	// DefaultMemorySize string with default size for memory
	DefaultMemorySize = resource.MustParse("1Gi")

	// DefaultPVSize default size for persistent volume
	DefaultPVSize = resource.MustParse("1Gi")

	DeploymentAnnotations = map[string]string{
		"openshift.io/display-name":      "Infinispan Cluster",
		"openshift.io/documentation-url": "http://infinispan.org/documentation/",
	}

	SystemPodLabels = map[string]bool{
		appsv1.StatefulSetPodNameLabel:  true,
		appsv1.StatefulSetRevisionLabel: true,
		StatefulSetPodLabel:             true,
	}

	JGroupsFastMerge = strings.ToUpper(GetEnvWithDefault("TEST_ENVIRONMENT", "false")) == "TRUE"
)

const (
	// DefaultOperatorUser users to access the cluster rest API
	DefaultOperatorUser = "operator"
	// DefaultDeveloperUser users to access the cluster rest API
	DefaultDeveloperUser = "developer"
	// DefaultCacheName default cache name for the CacheService
	DefaultCacheName         = "default"
	AdminUsernameKey         = "username"
	AdminPasswordKey         = "password"
	InfinispanAdminPort      = 11223
	InfinispanAdminPortName  = "infinispan-adm"
	InfinispanUserPortName   = "infinispan"
	InfinispanPingPort       = 8888
	InfinispanPingPortName   = "ping"
	InfinispanUserPort       = 11222
	CrossSitePort            = 7900
	CrossSitePortName        = "xsite"
	StatefulSetPodLabel      = "app.kubernetes.io/created-by"
	StaticCrossSiteUriSchema = "infinispan+xsite"
	// DefaultCacheManagerName default cache manager name used for cross site
	DefaultCacheManagerName                 = "default"
	CacheServiceFixedMemoryXmxMb            = 200
	CacheServiceJvmNativeMb                 = 220
	CacheServiceMinHeapFreeRatio            = 5
	CacheServiceMaxHeapFreeRatio            = 10
	CacheServiceJvmNativePercentageOverhead = 1
	CacheServiceMaxRamMb                    = CacheServiceFixedMemoryXmxMb + CacheServiceJvmNativeMb
	CacheServiceJavaOptions                 = "-Xmx%dM -Xms%dM -XX:MaxRAM=%dM -Dsun.zip.disableMemoryMapping=true -XX:+UseSerialGC -XX:MinHeapFreeRatio=%d -XX:MaxHeapFreeRatio=%d %s"
	CacheServiceNativeJavaOptions           = "-Xmx%dM -Xms%dM -Dsun.zip.disableMemoryMapping=true %s"

	NativeImageMarker                   = "native"
	GeneratedSecretSuffix               = "generated-secret"
	InfinispanFinalizer                 = "finalizer.infinispan.org"
	SiteServiceTemplate                 = "%v-site"
	ServerConfigRoot                    = "/etc/config"
	ServerEncryptRoot                   = "/etc/encrypt"
	ServerEncryptTruststoreRoot         = ServerEncryptRoot + "/truststore"
	ServerEncryptKeystoreRoot           = ServerEncryptRoot + "/keystore"
	SiteTransportKeyStoreRoot           = ServerEncryptRoot + "/transport-site-tls"
	SiteRouterKeyStoreRoot              = ServerEncryptRoot + "/router-site-tls"
	SiteTrustStoreRoot                  = ServerEncryptRoot + "/truststore-site-tls"
	ServerSecurityRoot                  = "/etc/security"
	ServerConfigFilename                = "infinispan.yaml"
	ServerConfigPath                    = ServerConfigRoot + "/" + ServerConfigFilename
	ServerIdentitiesFilename            = "identities.yaml"
	ServerAdminUsersPropertiesFilename  = "admin-users.properties"
	ServerAdminGroupsPropertiesFilename = "admin-groups.properties"
	ServerUsersPropertiesFilename       = "users.properties"
	ServerGroupsPropertiesFilename      = "groups.properties"
	CliPropertiesFilename               = "cli.properties"
	ServerIdentitiesCliFilename         = "identities.cli"
	ServerAdminIdentitiesRoot           = ServerSecurityRoot + "/admin"
	ServerAdminIdentitiesPath           = ServerAdminIdentitiesRoot + "/" + ServerIdentitiesFilename
	ServerUserIdentitiesRoot            = ServerSecurityRoot + "/user"
	ServerUserIdentitiesPath            = ServerUserIdentitiesRoot + "/" + ServerIdentitiesFilename
	ServerOperatorSecurity              = ServerSecurityRoot + "/conf/operator-security"
	ServerRoot                          = "/opt/infinispan/server"

	ServerHTTPBasePath          = "rest/v2"
	ServerHTTPCacheManagerPath  = ServerHTTPBasePath + "/cache-managers/" + DefaultCacheManagerName
	ServerHTTPHealthPath        = ServerHTTPCacheManagerPath + "/health"
	ServerHTTPServerStop        = ServerHTTPBasePath + "/server?action=stop"
	ServerHTTPClusterStop       = ServerHTTPBasePath + "/cluster?action=stop"
	ServerHTTPContainerShutdown = ServerHTTPBasePath + "/container?action=shutdown"
	ServerHTTPHealthStatusPath  = ServerHTTPHealthPath + "/status"
	ServerHTTPLoggersPath       = ServerHTTPBasePath + "/logging/loggers"
	ServerHTTPModifyLoggerPath  = ServerHTTPLoggersPath + "/%s?level=%s"
	ServerHTTPXSitePath         = ServerHTTPCacheManagerPath + "/x-site/backups"

	EncryptTruststoreKey         = "truststore.p12"
	EncryptTruststorePasswordKey = "truststore-password"

	DefaultCacheTemplate = `<infinispan>
		<cache-container>
			<distributed-cache name="%v" mode="SYNC" owners="%d" statistics="true">
				<memory>
					<off-heap size="%d" eviction="MEMORY" strategy="REMOVE"/>
				</memory>
				<partition-handling when-split="ALLOW_READ_WRITES" merge-policy="REMOVE_ALL" />
			</distributed-cache>
		</cache-container>
	</infinispan>`
)

const (
	// DefaultMinimumAutoscalePollPeriod minimum period for autoscaler polling loop
	DefaultMinimumAutoscalePollPeriod = 5 * time.Second
	//DefaultRequeueOnWrongSpec requeue delay on wrong values in Spec
	DefaultRequeueOnWrongSpec = 5 * time.Second
	//DefaultWaitOnCluster delay for the Infinispan cluster wait if it not created while Cache creation
	DefaultWaitOnCluster = 10 * time.Second
	// DefaultWaitOnCreateResource delay for wait until resource (Secret, ConfigMap, Service) is created
	DefaultWaitOnCreateResource = 2 * time.Second
	// DefaultLongWaitOnCreateResource delay for wait until non core resource is create (only Grafana CRD atm)
	DefaultLongWaitOnCreateResource = 60 * time.Second
	//DefaultWaitClusterNotWellFormed wait delay until cluster is not well formed
	DefaultWaitClusterNotWellFormed = 15 * time.Second
	// DefaultWaitPodsNotReady wait delay until cluster pods are ready
	DefaultWaitClusterPodsNotReady = 2 * time.Second
)

const (
	ExternalTypeService = "Service"
	ExternalTypeRoute   = "Route"
	ExternalTypeIngress = "Ingress"
	ServiceMonitorType  = "ServiceMonitor"
)

const DefaultKubeConfig = "~/.kube/config"

const (
	DefaultSiteKeyStoreFileName       = "keystore.p12"
	DefaultSiteTransportKeyStoreAlias = "transport"
	DefaultSiteRouterKeyStoreAlias    = "router"
	DefaultSiteTrustStoreFileName     = "truststore.p12"
)

// GetWithDefault return value if not empty else return defValue
func GetWithDefault(value, defValue string) string {
	if value == "" {
		return defValue
	}
	return value
}

// GetEnvWithDefault return os.Getenv(name) if exists else return defValue
func GetEnvWithDefault(name, defValue string) string {
	return GetWithDefault(os.Getenv(name), defValue)
}
