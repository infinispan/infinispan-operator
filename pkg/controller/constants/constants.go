package constants

import (
	"strings"
	"time"

	"github.com/infinispan/infinispan-operator/pkg/controller/utils/common"
	"k8s.io/apimachinery/pkg/api/resource"
)

var (
	// DefaultImageName is used if a specific image name is not provided
	DefaultImageName = common.GetEnvWithDefault("DEFAULT_IMAGE", "infinispan/server:latest")

	// InitContainerImageName allows a custom initContainer image to be used
	InitContainerImageName = common.GetEnvWithDefault("INITCONTAINER_IMAGE", "busybox")

	// JGroupsDiagnosticsFlag is used to enable traces for JGroups
	JGroupsDiagnosticsFlag = strings.ToUpper(common.GetEnvWithDefault("JGROUPS_DIAGNOSTICS", "FALSE"))

	// DefaultMemorySize string with default size for memory
	DefaultMemorySize = resource.MustParse("512Mi")

	// DefaultPVSize default size for persistent volume
	DefaultPVSize = resource.MustParse("1Gi")

	// DefaultCPUSize string with default size for CPU
	DefaultCPULimit int64 = 500

	DeploymentAnnotations = map[string]string{
		"description":                    "Infinispan 10 (Ephemeral)",
		"iconClass":                      "icon-infinispan",
		"openshift.io/display-name":      "Infinispan 10 (Ephemeral)",
		"openshift.io/documentation-url": "http://infinispan.org/documentation/",
	}
)

const (
	// DefaultOperatorUser users to access the cluster rest API
	DefaultOperatorUser = "operator"
	// DefaultDeveloperUser users to access the cluster rest API
	DefaultDeveloperUser = "developer"
	// DefaultCacheName default cache name for the CacheService
	DefaultCacheName   = "default"
	InfinispanPort     = 11222
	InfinispanPingPort = 8888
	CrossSitePort      = 7900
	// DefaultCacheManagerName default cache manager name used for cross site
	DefaultCacheManagerName                 = "default"
	CacheServiceFixedMemoryXmxMb            = 200
	CacheServiceJvmNativeMb                 = 220
	CacheServiceMaxRamMb                    = CacheServiceFixedMemoryXmxMb + CacheServiceJvmNativeMb
	CacheServiceAdditionalJavaOptions       = "-Dsun.zip.disableMemoryMapping=true -XX:+UseSerialGC -XX:MinHeapFreeRatio=5 -XX:MaxHeapFreeRatio=10"
	CacheServiceJvmNativePercentageOverhead = 1

	ServerHTTPBasePath         = "rest/v2"
	ServerHTTPClusterStop      = ServerHTTPBasePath + "/cluster?action=stop"
	ServerHTTPHealthPath       = ServerHTTPBasePath + "/cache-managers/default/health"
	ServerHTTPHealthStatusPath = ServerHTTPHealthPath + "/status"

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

	// Using for local run and test only
	ClusterUpKubeConfig = "../../openshift.local.clusterup/kube-apiserver/admin.kubeconfig"
)

const (
	// DefaultMinimumAutoscalePollPeriod minimun period for autocaler polling loop
	DefaultMinimumAutoscalePollPeriod = 5 * time.Second
	//DefaultRequeueOnCreateExposeServiceDelay requeue delay before retry exposed service creation
	DefaultRequeueOnCreateExposeServiceDelay = 5 * time.Second
	//DefaultRequeueOnWrongSpec requeue delay on wrong values in Spec
	DefaultRequeueOnWrongSpec = 5 * time.Second
)
