package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	infinispanv1 "github.com/infinispan/infinispan-operator/api/v1"
	"github.com/infinispan/infinispan-operator/controllers/constants"
	"github.com/infinispan/infinispan-operator/pkg/infinispan/client/api"
	kube "github.com/infinispan/infinispan-operator/pkg/kubernetes"
	"github.com/prometheus/common/log"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

var autoscaleThreadPool = struct {
	sync.RWMutex
	m map[types.NamespacedName]int
}{m: make(map[types.NamespacedName]int)}

// Starting a go routine that does polling autoscaling on an Infinispan cluster
func (r *infinispanRequest) addAutoscalingEquipment() {
	clusterNsn := r.req.NamespacedName
	autoscaleThreadPool.Lock()
	_, ok := autoscaleThreadPool.m[clusterNsn]
	autoscaleThreadPool.Unlock()
	if !ok {
		autoscaleThreadPool.Lock()
		autoscaleThreadPool.m[clusterNsn] = 1
		autoscaleThreadPool.Unlock()
		go r.autoscalerLoop()
	}
}

func (r *infinispanRequest) autoscalerLoop() {
	clusterNsn := r.req.NamespacedName
	log.Info(fmt.Sprintf("Starting loop for autoscaling on cluster %v", clusterNsn))
	for {
		time.Sleep(constants.DefaultMinimumAutoscalePollPeriod)
		ispn := infinispanv1.Infinispan{}
		// Check all the cluster in the namespace for autoscaling
		err := r.Client.Get(r.ctx, clusterNsn, &ispn)
		if err != nil {
			if errors.IsNotFound(err) {
				// Ispn cluster doesn't exists any more
				autoscaleThreadPool.Lock()
				delete(autoscaleThreadPool.m, clusterNsn)
				autoscaleThreadPool.Unlock()
				// leaving the loop
				log.Info(fmt.Sprintf("Stopping loop for autoscaling on cluster %v. Cluster deleted.", clusterNsn))
				break
			}
			log.Error(err, "Unable to select cluster pods list")
			continue
		}
		// Skip this cluster if autoscale is not enabled or ServiceType is not CacheService
		if ispn.Spec.Autoscale == nil || !ispn.IsCache() {
			// Autoscale not needed, leaving the loop
			autoscaleThreadPool.Lock()
			delete(autoscaleThreadPool.m, clusterNsn)
			autoscaleThreadPool.Unlock()
			log.Info(fmt.Sprintf("Stopping loop for autoscaling on cluster %v. Autoscaling disabled.", clusterNsn))
			break
		}
		if !ispn.IsWellFormed() || ispn.Spec.Autoscale.Disabled {
			// Skip autoscale disabled and not well formed clusters
			continue
		}

		// Min # of required pods, same value for all the cluster pods
		metricMinPodNum := int32(0)
		// Data memory percent usage array, one value per pod
		metricDataMemoryPercentUsed := map[string]int{}
		podList := &corev1.PodList{}
		if err = r.kubernetes.ResourcesList(ispn.Namespace, ispn.Labels(""), podList, r.ctx); err != nil {
			continue
		}

		var skipAutoscale bool
		for _, pItem := range podList.Items {
			podName := pItem.Name

			ispnClient, err := NewInfinispanForPod(r.ctx, podName, &ispn, r.kubernetes)
			if err != nil {
				log.Error(err, "unable to create Infinispan client", "podName", podName)
				skipAutoscale = true
				break
			}

			if metricMinPodNum == 0 {
				metricMinPodNum, err = getMetricMinPodNum(podName, ispnClient.Metrics())
				if err != nil {
					log.Error(err, "Unable to get metricMinPodNum for pod", "podName", podName)
				}
			}
			err = getMetricDataMemoryPercentUsage(&metricDataMemoryPercentUsed, podName, ispnClient.Metrics())
			if err != nil {
				log.Error(err, "Unable to get DataMemoryUsed for pod", "podName", podName)
			}
		}

		if !skipAutoscale {
			autoscaleOnPercentUsage(r.ctx, &metricDataMemoryPercentUsed, metricMinPodNum, &ispn, r.kubernetes)
		}
	}
}

// getMetricMinPodNum get the minimum number of nodes required to avoid data lost
func getMetricMinPodNum(podName string, metrics api.Metrics) (int32, error) {
	res, err := metrics.Get("vendor/cache_manager_default_cache_default_cluster_cache_stats_required_minimum_number_of_nodes")
	if err != nil {
		return 0, err
	}
	log.Info(res.String())
	minNumOfNodes := map[string]int32{}
	err = json.Unmarshal(res.Bytes(), &minNumOfNodes)
	if err != nil {
		return 0, err
	}
	// We expect 1 value
	if len(minNumOfNodes) != 1 {
		return 0, fmt.Errorf("more than 1 value returned for minNumOfNodes")
	}
	ret := int32(0)
	for _, v := range minNumOfNodes {
		ret = v
	}
	return ret, nil
}

func getMetricDataMemoryPercentUsage(m *map[string]int, podName string, metrics api.Metrics) error {
	res, err := metrics.Get("vendor/cache_manager_default_cache_container_stats_data_memory_used")
	if err != nil {
		return err
	}
	log.Info(res.String())
	usedMap := map[string]int{}
	err = json.Unmarshal(res.Bytes(), &usedMap)
	if err != nil || len(usedMap) < 1 {
		return err
	}
	var used int
	for _, v := range usedMap {
		used = v
		break
	}

	res, err = metrics.Get("vendor/cache_manager_default_cache_default_configuration_eviction_size")
	if err != nil {
		return err
	}
	log.Info(res.String())
	totalMap := map[string]int{}
	err = json.Unmarshal(res.Bytes(), &totalMap)
	if err != nil || len(totalMap) < 1 {
		return err
	}
	var total int
	for _, v := range totalMap {
		total = v
		break
	}

	(*m)["dataMemPercentUsage;node="+podName] = int(used * 100 / total)
	log.Info("Current memory usage percent", "value", (*m)["dataMemPercentUsage;node="+podName])
	return nil
}

func autoscaleOnPercentUsage(ctx context.Context, usage *map[string]int, minPodNum int32, ispn *infinispanv1.Infinispan, kubernetes *kube.Kubernetes) {
	hiTh := ispn.Spec.Autoscale.MaxMemUsagePercent
	loTh := ispn.Spec.Autoscale.MinMemUsagePercent
	maxReplicas := ispn.Spec.Autoscale.MaxReplicas
	if maxReplicas != 0 {
		upscale := false
		downscale := true
		// Logic here is:
		// donwscale if all the pods are below lower threshold
		// upscale if just one pod is over the high threshold
		for _, v := range *usage {
			log.Info("Current memory usage percent", "value", v)
			if v < loTh {
				continue
			}
			downscale = false
			if v > hiTh {
				upscale = true
				break
			}
		}
		// Downscale or upscale acting directly on infinispan.spec.replicas field
		if upscale && ispn.Spec.Replicas < ispn.Spec.Autoscale.MaxReplicas {
			ispn.Spec.Replicas++
			err := kubernetes.Client.Update(ctx, ispn)
			if err != nil {
				log.Error(err, "Unable to upscale")
			}
			log.Info("Upscaling cluster", "Name", ispn.Name, "New Replicas", ispn.Spec.Replicas)
			return
		}
		if downscale && ispn.Spec.Replicas > minPodNum && ispn.Spec.Replicas > ispn.Spec.Autoscale.MinReplicas {
			ispn.Spec.Replicas--
			err := kubernetes.Client.Update(ctx, ispn)
			if err != nil {
				log.Error(err, "Unable to downscale")
			}
			log.Info("Downscaling cluster", "Name", ispn.Name, "New Replicas", ispn.Spec.Replicas)
			return
		}
	}
}
