package infinispan

import (
	"context"

	"github.com/go-logr/logr"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	ispn "github.com/infinispan/infinispan-operator/pkg/infinispan"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func applyLabelsToCoordinatorsPod(podList *corev1.PodList, cluster ispn.ClusterInterface, client client.Client, logger logr.Logger) bool {
	coordinatorFound := false
	for _, item := range podList.Items {
		cacheManagerInfo, err := cluster.GetCacheManagerInfo(consts.DefaultCacheManagerName, item.Name)
		if err == nil {
			lab, ok := item.Labels[consts.CoordinatorPodLabel]
			if cacheManagerInfo[consts.CoordinatorPodLabel].(bool) {
				if !ok || lab != "true" {
					item.Labels[consts.CoordinatorPodLabel] = "true"
					err = client.Update(context.TODO(), &item)
				}
				coordinatorFound = (err == nil)
			} else {
				if ok && lab == "true" {
					// If present leave the label but false the value
					if ok {
						item.Labels[consts.CoordinatorPodLabel] = "false"
						err = client.Update(context.TODO(), &item)
					}
				}
			}
		}
		if err != nil {
			logger.Error(err, "Generic error in managing x-site coordinators")
		}
	}
	return coordinatorFound
}
