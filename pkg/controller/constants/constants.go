package constants

import (
	"os"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
)

var (
	// DefaultImageName is used if a specific image name is not provided
	DefaultImageName = GetDefaultInfinispanJavaImage()

	// DefaultMemorySize string with default size for memory
	DefaultMemorySize = resource.MustParse("512Mi")

	// DefaultPVSize default size for persistent volume
	DefaultPVSize = resource.MustParse("1Gi")

	// DefaultCPULimit string with default size for CPU
	DefaultCPULimit int64 = 500

	DeploymentAnnotations = map[string]string{
		"openshift.io/display-name":      "Infinispan Cluster",
		"openshift.io/documentation-url": "http://infinispan.org/documentation/",
	}
)

const (
	// DefaultOperatorUser users to access the cluster rest API
	DefaultOperatorUser = "operator"
	// DefaultDeveloperUser users to access the cluster rest API
	DefaultDeveloperUser = "developer"
	// DefaultCacheName default cache name for the CacheService
	DefaultCacheName = "default"
	// DefaultCacheManagerName default cache manager name used for cross site
	DefaultCacheManagerName                 = "default"
	CacheServiceFixedMemoryXmxMb            = 200
	CacheServiceJvmNativeMb                 = 220
	CacheServiceMaxRamMb                    = CacheServiceFixedMemoryXmxMb + CacheServiceJvmNativeMb
	CacheServiceAdditionalJavaOptions       = "-Dsun.zip.disableMemoryMapping=true -XX:+UseSerialGC -XX:MinHeapFreeRatio=5 -XX:MaxHeapFreeRatio=10"
	CacheServiceJvmNativePercentageOverhead = 1
	InfinispanFinalizer                     = "finalizer.infinispan.org"

	ServerHTTPBasePath         = "rest/v2"
	ServerHTTPClusterStop      = ServerHTTPBasePath + "/cluster?action=stop"
	ServerHTTPHealthPath       = ServerHTTPBasePath + "/cache-managers/default/health"
	ServerHTTPHealthStatusPath = ServerHTTPHealthPath + "/status"
)

const (
	// DefaultMinimumAutoscalePollPeriod minimun period for autocaler polling loop
	DefaultMinimumAutoscalePollPeriod = 5 * time.Second
	//DefaultRequeueOnCreateExposeServiceDelay requeue delay before retry exposed service creation
	DefaultRequeueOnCreateExposeServiceDelay = 5 * time.Second
	//DefaultRequeueOnWrongSpec requeue delay on wrong values in Spec
	DefaultRequeueOnWrongSpec = 5 * time.Second
	//DefaultWaitClusterNotWellFormed wait delay until cluster is not well formed
	DefaultWaitClusterNotWellFormed = 15 * time.Second
)

const DefaultKubeConfig = "~/.kube/config"

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
