package infinispan

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	infinispanv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	"github.com/infinispan/infinispan-operator/pkg/controller/constants"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// Starting a go routine that does polling autoscaling
func addAutoscalingEquipment(mgr manager.Manager, r reconcile.Reconciler) {
	go autoscalerPoll(mgr, r)
}

var defaultMinimumPollPeriod = time.Second

func autoscalerPoll(mgr manager.Manager, r reconcile.Reconciler) {
	log.Info("Starting adhoc loop for autoscaling")
	for true {
		time.Sleep(defaultMinimumPollPeriod)
		ispnList := infinispanv1.InfinispanList{}
		// Check all the cluster in the namespace for autoscaling
		err := mgr.GetClient().List(context.TODO(), &ispnList)
		if err != nil {
			log.Error(err, "Unable to select cluster pods list")
			continue
		}
		for _, iItem := range ispnList.Items {
			// Skip this cluster if autoscale is not enabled or ServiceType is not CacheService
			if iItem.Spec.Autoscale == nil || iItem.Spec.Service.Type != infinispanv1.ServiceTypeCache {
				continue
			}
			if !iItem.IsConditionTrue("wellFormed") {
				// Skip not well formed clusters
				continue
			}
			// We need the password and the scheme to get the metrics
			pass, err := cluster.GetPassword(constants.DefaultOperatorUser, iItem.Spec.Security.EndpointSecretName, iItem.Namespace)
			protocol := iItem.GetEndpointScheme()
			if err != nil {
				continue
			}

			// Min # of required pods, same value for all the cluster pods
			metricMinPodNum := int32(0)
			// Data memory percent usage array, one value per pod
			metricDataMemoryPercentUsed := map[string]int{}
			podList := &corev1.PodList{}
			kubernetes.GetK8sResources(iItem.ObjectMeta.Name, iItem.ObjectMeta.Namespace, podList)

			for _, pItem := range podList.Items {
				// time.Sleep(time.Duration(10000/len(podList.Items)) * time.Millisecond)
				if metricMinPodNum == 0 {
					metricMinPodNum, err = getMetricMinPodNum(consts.DefaultOperatorUser, pass, string(protocol), pItem)
					if err != nil {
						log.Error(err, "Unable to get metricMinPodNum for pod", "podName", pItem.Name)
					}
				}
				err = getMetricDataMemoryPercentUsage(&metricDataMemoryPercentUsed, consts.DefaultOperatorUser, pass, string(protocol), pItem)
				if err != nil {
					log.Error(err, "Unable to get DataMemoryUsed for pod", "podName", pItem.Name)
				}
			}
			autoscaleOnPercentUsage(&metricDataMemoryPercentUsed, metricMinPodNum, &iItem)
		}
	}
}

// getMetricMinPodNum get the minimum number of nodes required to avoid data lost
func getMetricMinPodNum(user, pass, protocol string, pItem corev1.Pod) (int32, error) {
	res, err := cluster.GetMetrics(user, pass, pItem.Name, pItem.Namespace, string(protocol), "/vendor/cache_manager_default_cache_default_cluster_cache_stats_required_minimum_number_of_nodes")
	if err != nil {
		return 0, err
	}
	log.Info(string(res.Bytes()))
	minNumOfNodes := map[string]int32{}
	err = json.Unmarshal(res.Bytes(), &minNumOfNodes)
	if err != nil {
		return 0, err
	}
	// We expect 1 value
	if len(minNumOfNodes) != 1 {
		return 0, fmt.Errorf("More than 1 value returned for minNumOfNodes")
	}
	ret := int32(0)
	for _, v := range minNumOfNodes {
		ret = v
	}
	return ret, nil
}

func getMetricDataMemoryPercentUsage(m *map[string]int, user, pass, protocol string, pItem corev1.Pod) error {
	res, err := cluster.GetMetrics(user, pass, pItem.Name, pItem.Namespace, string(protocol), "/vendor/cache_manager_default_cache_default_statistics_data_memory_used")
	if err != nil {
		return err
	}
	log.Info(string(res.Bytes()))
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

	res, err = cluster.GetMetrics(user, pass, pItem.Name, pItem.Namespace, string(protocol), "/vendor/cache_manager_default_cache_default_configuration_eviction_size")
	if err != nil {
		return err
	}
	log.Info(string(res.Bytes()))
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

	(*m)["dataMemPercentUsage;node="+pItem.Name] = int(used * 100 / total)
	log.Info("Current memory usage percent", "value", (*m)["dataMemPercentUsage;node="+pItem.Name])
	return nil
}

func autoscaleOnPercentUsage(usage *map[string]int, minPodNum int32, ispn *infinispanv1.Infinispan) {
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
			err := kubernetes.Client.Update(context.TODO(), ispn)
			if err != nil {
				log.Error(err, "Unable to upscale")
			}
			log.Info("Upscaling cluster", "Name", ispn.Name, "New Replicas", ispn.Spec.Replicas)
			return
		}
		if downscale && ispn.Spec.Replicas > minPodNum && ispn.Spec.Replicas > ispn.Spec.Autoscale.MinReplicas {
			ispn.Spec.Replicas--
			err := kubernetes.Client.Update(context.TODO(), ispn)
			if err != nil {
				log.Error(err, "Unable to downscale")
			}
			log.Info("Downscaling cluster", "Name", ispn.Name, "New Replicas", ispn.Spec.Replicas)
			return
		}
	}
}
