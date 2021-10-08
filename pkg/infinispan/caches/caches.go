package caches

import (
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	infinispanv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	ispn "github.com/infinispan/infinispan-operator/pkg/infinispan"
)

// DefaultCacheTemplateXML return default template for cache
func DefaultCacheTemplateXML(podName string, infinispan *infinispanv1.Infinispan, cluster ispn.ClusterInterface, logger logr.Logger) (string, error) {
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

func CreateCacheFromDefault(podName string, infinispan *infinispanv1.Infinispan, cluster ispn.ClusterInterface, logger logr.Logger) error {
	defaultCacheXML, err := DefaultCacheTemplateXML(podName, infinispan, cluster, logger)
	if err != nil {
		return err
	}
	return cluster.CreateCacheWithTemplate(consts.DefaultCacheName, defaultCacheXML, podName)
}

// ConnectCaches Connects caches from a cluster (target) to another (source) via Remote Stores. Caches are created in the target cluster if needed.
func ConnectCaches(podNameTarget, podNameSource, adminPasswordSource, sourceIp string, logger logr.Logger, cluster ispn.ClusterInterface) error {
	// Obtain all cache names from the source cluster
	names, err := cluster.CacheNames(podNameSource)
	if err != nil {
		return fmt.Errorf("failed to get cache names from the source cluster: %w", err)
	}

	logger.Info(fmt.Sprintf("Cache names in the source cluster '%s'", names))
	logger.Info("Creating caches in the target cluster")
	for _, cacheName := range names {
		// Create all caches in the new cluster
		if !strings.HasPrefix(cacheName, "org.infinispan") {
			exists, err := cluster.ExistsCache(cacheName, podNameTarget)
			if err != nil {
				return fmt.Errorf("failed to check cache existence '%s': %w", cacheName, err)
			}
			if !exists {
				configuration, err := cluster.GetCacheConfig(cacheName, podNameSource)
				if err != nil {
					return fmt.Errorf("failed to get cache '%s' config from source cluster: %w", cacheName, err)
				}
				err = cluster.CreateCache(cacheName, configuration, podNameTarget)
				if err != nil {
					return fmt.Errorf("failed to create cache '%s': %w", cacheName, err)
				}
				logger.Info(fmt.Sprintf("Cache '%s' created", cacheName))
			} else {
				logger.Info(fmt.Sprintf("Cache '%s' already exists", cacheName))
			}
		}
		// Add a remote store to each cache pointing to the source cluster
		connected, err := cluster.IsSourceConnected(cacheName, podNameTarget)
		if err != nil {
			return fmt.Errorf("failed to call source-connected from target cluster for cache '%s': %w", cacheName, err)
		}
		if !connected {
			remoteStoreCfg, err := CreateRemoteStoreConfig(sourceIp, cacheName, adminPasswordSource)
			if err != nil {
				return fmt.Errorf("failed to generate remote store config '%s': %w", cacheName, err)
			}
			logger.Info(remoteStoreCfg)
			err = cluster.AddRemoteStore(cacheName, remoteStoreCfg, podNameTarget)
			if err != nil {
				return fmt.Errorf("failed to add remote store to cache '%s': %w", cacheName, err)
			}
		} else {
			logger.Info(fmt.Sprintf("Cache '%s' already connected to remote cluster", cacheName))
		}

	}
	return nil
}

// SyncCaches Do a sync data operation for each cache in a cluster
func SyncCaches(podName string, logger logr.Logger, cluster ispn.ClusterInterface) error {
	names, err := cluster.CacheNames(podName)
	if err != nil {
		return fmt.Errorf("failed to get cache names from the target cluster: %w", err)
	}

	logger.Info(fmt.Sprintf("Cache names in the target cluster '%s'", names))

	for _, cacheName := range names {
		count, err := cluster.SyncData(cacheName, podName)
		if err != nil {
			return fmt.Errorf("failed to sync data for cache'%s': %w", cacheName, err)
		}
		logger.Info(fmt.Sprintf("Sync result %s:'", count))

		err = cluster.DisconnectSource(cacheName, podName)
		if err != nil {
			return fmt.Errorf("failed to disconnect source for cache'%s': %w", cacheName, err)
		}
		logger.Info("Disconnected source")

	}
	return nil
}
