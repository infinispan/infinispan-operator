package cache

import (
	"fmt"

	"github.com/go-logr/logr"
	infinispanv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	ispnutil "github.com/infinispan/infinispan-operator/pkg/controller/infinispan/util"
)

// GetDefaultCacheTemplateXML return default template for cache
func GetDefaultCacheTemplateXML(podName string, infinispan *infinispanv1.Infinispan, cluster ispnutil.ClusterInterface, logger logr.Logger) (string, error) {
	memoryLimitBytes, err := cluster.GetMemoryLimitBytes(podName)
	if err != nil {
		logger.Error(err, "unable to extract memory limit (bytes) from pod")
		return "", err
	}

	maxUnboundedMemory, err := cluster.GetMaxMemoryUnboundedBytes(podName)
	if err != nil {
		logger.Error(err, "unable to extract max memory unbounded from pod")
		return "", err
	}

	containerMaxMemory := maxUnboundedMemory
	if memoryLimitBytes < maxUnboundedMemory {
		containerMaxMemory = memoryLimitBytes
	}

	nativeMemoryOverhead := containerMaxMemory * (consts.CacheServiceJvmNativePercentageOverhead / 100)
	evictTotalMemoryBytes := containerMaxMemory - (consts.CacheServiceJvmNativeMb * 1024 * 1024) - (consts.CacheServiceFixedMemoryXmxMb * 1024 * 1024) - nativeMemoryOverhead
	replicationFactor := infinispan.Spec.Service.ReplicationFactor

	logger.Info("calculated maximum off-heap size", "size", evictTotalMemoryBytes, "container max memory", containerMaxMemory, "memory limit (bytes)", memoryLimitBytes, "max memory bound", maxUnboundedMemory)

	return fmt.Sprintf(consts.DefaultCacheTemplate, consts.DefaultCacheName, replicationFactor, evictTotalMemoryBytes), nil
}

func CreateCacheServiceDefaultCache(podName string, infinispan *infinispanv1.Infinispan, kubernetes *ispnutil.Kubernetes, cluster ispnutil.ClusterInterface, logger logr.Logger) error {
	defaultCacheXML, err := GetDefaultCacheTemplateXML(podName, infinispan, cluster, logger)
	if err != nil {
		return err
	}
	return cluster.CreateCacheWithTemplate(consts.DefaultCacheName, defaultCacheXML, podName)
}

func ExistsCacheServiceDefaultCache(podName string, infinispan *infinispanv1.Infinispan, kubernetes *ispnutil.Kubernetes, cluster ispnutil.ClusterInterface) (bool, error) {
	return cluster.ExistsCache(consts.DefaultCacheName, podName)
}
