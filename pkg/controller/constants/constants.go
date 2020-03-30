package constants

import (
	"github.com/infinispan/infinispan-operator/pkg/controller/utils/common"
	"k8s.io/apimachinery/pkg/api/resource"
)

var (
	// DefaultImageName is used if a specific image name is not provided
	DefaultImageName = common.GetEnvWithDefault("DEFAULT_IMAGE", "infinispan/server:latest")

	// DefaultMemorySize string with default size for memory
	DefaultMemorySize = resource.MustParse("512Mi")

	// DefaultPVSize default size for persistent volume
	DefaultPVSize = resource.MustParse("1Gi")

	// DefaultCPUSize string with default size for CPU
	DefaultCPULimit int64 = 500
)

const (
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
