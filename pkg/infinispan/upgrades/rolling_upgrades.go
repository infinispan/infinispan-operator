package upgrades

import (
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/configuration/container"
	"github.com/infinispan/infinispan-operator/pkg/mime"
)

// ConnectCaches Connects caches from a cluster (target) to another (source) via Remote Stores. Caches are created in the target cluster if needed.
func ConnectCaches(user, adminPasswordSource, sourceIp string, sourceClient, targetClient api.Infinispan, logger logr.Logger) error {

	// Obtain all cache names from the source cluster
	names, err := sourceClient.Caches().Names()
	if err != nil {
		return fmt.Errorf("failed to get cache names from the source cluster: %w", err)
	}

	logger.Info(fmt.Sprintf("Cache names in the source cluster '%s'", names))
	logger.Info("Creating caches in the target cluster")
	configType := mime.ApplicationJson
	for _, cacheName := range names {
		targetCache := targetClient.Cache(cacheName)
		// Create all caches in the new cluster
		if !strings.HasPrefix(cacheName, "org.infinispan") {
			exists, err := targetCache.Exists()
			if err != nil {
				return fmt.Errorf("failed to check cache existence '%s': %w", cacheName, err)
			}
			if !exists {
				config, err := sourceClient.Cache(cacheName).Config(configType)
				if err != nil {
					return fmt.Errorf("failed to get cache '%s' config from source cluster: %w", cacheName, err)
				}
				if err = targetCache.Create(config, configType); err != nil {
					return fmt.Errorf("failed to create cache '%s': %w", cacheName, err)
				}
				logger.Info(fmt.Sprintf("Cache '%s' created", cacheName))
			} else {
				logger.Info(fmt.Sprintf("Cache '%s' already exists", cacheName))
			}
		}
		// Add a remote store to each cache pointing to the source cluster
		rollingUpgrade := targetCache.RollingUpgrade()
		connected, err := rollingUpgrade.SourceConnected()
		if err != nil {
			return fmt.Errorf("failed to call source-connected from target cluster for cache '%s': %w", cacheName, err)
		}
		if !connected {
			remoteStoreCfg, err := container.CreateRemoteStoreConfig(sourceIp, cacheName, user, adminPasswordSource)
			if err != nil {
				return fmt.Errorf("failed to generate remote store config '%s': %w", cacheName, err)
			}
			logger.Info(remoteStoreCfg)
			if err = rollingUpgrade.AddSource(remoteStoreCfg, configType); err != nil {
				return fmt.Errorf("failed to add remote store to cache '%s': %w", cacheName, err)
			}
		} else {
			logger.Info(fmt.Sprintf("Cache '%s' already connected to remote cluster", cacheName))
		}

	}
	return nil
}

// SyncCaches Do a sync data operation for each cache in a cluster
func SyncCaches(targetClient api.Infinispan, logger logr.Logger) error {
	names, err := targetClient.Caches().Names()
	if err != nil {
		return fmt.Errorf("failed to get cache names from the target cluster: %w", err)
	}
	logger.Info(fmt.Sprintf("Cache names in the target cluster '%s'", names))

	for _, cacheName := range names {
		upgrade := targetClient.Cache(cacheName).RollingUpgrade()
		count, err := upgrade.SyncData()
		if err != nil {
			return fmt.Errorf("failed to sync data for cache'%s': %w", cacheName, err)
		}
		logger.Info(fmt.Sprintf("Sync result %s:'", count))

		if err = upgrade.DisconnectSource(); err != nil {
			return fmt.Errorf("failed to disconnect source for cache'%s': %w", cacheName, err)
		}
		logger.Info("Disconnected source")
	}
	return nil
}
