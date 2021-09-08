package controllers

import (
	"fmt"
	"strconv"
	"strings"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	ispn "github.com/infinispan/infinispan-operator/pkg/infinispan"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (r *infinispanRequest) applyLabelsToCoordinatorsPod(podList *corev1.PodList, siteLocations []string, cluster ispn.ClusterInterface) (*ispnv1.InfinispanCondition, error) {
	for _, item := range podList.Items {
		cacheManagerInfo, err := cluster.GetCacheManagerInfo(consts.DefaultCacheManagerName, item.Name)
		if err == nil {
			lab, ok := item.Labels[consts.CoordinatorPodLabel]
			if cacheManagerInfo.Coordinator {
				if !ok || lab != strconv.FormatBool(cacheManagerInfo.Coordinator) {
					item.Labels[consts.CoordinatorPodLabel] = strconv.FormatBool(cacheManagerInfo.Coordinator)
					if err = r.Client.Update(r.ctx, &item); err != nil {
						return nil, err
					}
				}
				// Perform cross-site view validation
				crossSiteViewFormed := &ispnv1.InfinispanCondition{Type: ispnv1.ConditionCrossSiteViewFormed, Status: metav1.ConditionTrue}
				sitesView, err := cacheManagerInfo.GetSitesView()
				if err == nil {
					for _, location := range siteLocations {
						if !sitesView[location] {
							crossSiteViewFormed.Status = metav1.ConditionFalse
							crossSiteViewFormed.Message = fmt.Sprintf("Site '%s' not ready", location)
							break
						}
					}
					if crossSiteViewFormed.Status == metav1.ConditionTrue {
						crossSiteViewFormed.Message = fmt.Sprintf("Cross-Site view: %s", strings.Join(siteLocations, ","))
					}
				} else {
					crossSiteViewFormed.Status = metav1.ConditionUnknown
					crossSiteViewFormed.Message = fmt.Sprintf("Error: %s", err.Error())
				}
				return crossSiteViewFormed, nil
			} else {
				if ok && lab == strconv.FormatBool(ok) {
					// If present leave the label but false the value
					if ok {
						item.Labels[consts.CoordinatorPodLabel] = strconv.FormatBool(cacheManagerInfo.Coordinator)
						if err = r.Client.Update(r.ctx, &item); err != nil {
							return nil, err
						}
					}
				}
			}
		}
	}
	return &ispnv1.InfinispanCondition{Type: ispnv1.ConditionCrossSiteViewFormed, Status: metav1.ConditionFalse, Message: "Coordinator not ready"}, nil
}
