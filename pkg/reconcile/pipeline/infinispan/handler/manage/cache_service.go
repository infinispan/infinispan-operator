package manage

import (
	"fmt"

	"github.com/go-logr/logr"
	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/infinispan/infinispan-operator/pkg/mime"
	pipeline "github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan"
	"github.com/infinispan/infinispan-operator/pkg/reconcile/pipeline/infinispan/handler/provision"
)

func CacheService(i *ispnv1.Infinispan, ctx pipeline.Context) {
	log := ctx.Log()

	ispnPods, err := ctx.InfinispanPods()
	if err != nil {
		return
	}

	ispnClient, err := ctx.InfinispanClient()
	if err != nil {
		ctx.Requeue(err)
		return
	}

	cacheClient := ispnClient.Cache(consts.DefaultCacheName)
	if existsCache, err := cacheClient.Exists(); err != nil {
		log.Error(err, "failed to validate default cache for cache service")
		ctx.Requeue(err)
		return
	} else if !existsCache {
		log.Info("createDefaultCache")
		defaultXml, err := DefaultCacheTemplateXML(ispnPods.Items[0].Name, i, ctx.Kubernetes(), ctx.Log())
		if err != nil {
			ctx.Requeue(err)
			return
		}

		if err = cacheClient.Create(defaultXml, mime.ApplicationXml); err != nil {
			log.Error(err, "failed to create default cache for cache service")
			ctx.Requeue(err)
			return
		}
	}
}

func DefaultCacheTemplateXML(podName string, infinispan *ispnv1.Infinispan, k8 *kube.Kubernetes, logger logr.Logger) (string, error) {
	namespace := infinispan.Namespace
	memoryLimitBytes, err := kube.GetPodMemoryLimitBytes(provision.InfinispanContainer, podName, namespace, k8)
	if err != nil {
		logger.Error(err, "unable to extract memory limit (bytes) from pod")
		return "", err
	}

	maxUnboundedMemory, err := kube.GetPodMaxMemoryUnboundedBytes(provision.InfinispanContainer, podName, namespace, k8)
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
