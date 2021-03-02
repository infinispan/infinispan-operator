package infinispan

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	ispnv1 "github.com/infinispan/infinispan-operator/pkg/apis/infinispan/v1"
	consts "github.com/infinispan/infinispan-operator/pkg/controller/constants"
	ispn "github.com/infinispan/infinispan-operator/pkg/infinispan"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (r *ReconcileInfinispan) applyLabelsToCoordinatorsPod(podList *corev1.PodList, siteLocations []ispnv1.InfinispanSiteLocationSpec, cluster ispn.ClusterInterface) (*ispnv1.InfinispanCondition, error) {
	for _, item := range podList.Items {
		cacheManagerInfo, err := cluster.GetCacheManagerInfo(consts.DefaultCacheManagerName, item.Name)
		if err == nil {
			lab, ok := item.Labels[consts.CoordinatorPodLabel]
			if cacheManagerInfo.Coordinator {
				if !ok || lab != strconv.FormatBool(cacheManagerInfo.Coordinator) {
					item.Labels[consts.CoordinatorPodLabel] = strconv.FormatBool(cacheManagerInfo.Coordinator)
					if err = r.client.Update(context.TODO(), &item); err != nil {
						return nil, err
					}
				}
				// Perform cross-site view validation
				crossSiteViewFormed := &ispnv1.InfinispanCondition{Type: ispnv1.ConditionCrossSiteViewFormed, Status: metav1.ConditionTrue}
				sitesView, err := cacheManagerInfo.GetSitesView()
				if err == nil {
					sites := make([]string, len(siteLocations))
					for i, location := range siteLocations {
						sites[i] = location.Name
						if !sitesView[location.Name] {
							crossSiteViewFormed.Status = metav1.ConditionFalse
							crossSiteViewFormed.Message = fmt.Sprintf("Site '%s' not ready", location.Name)
							break
						}
					}
					if crossSiteViewFormed.Status == metav1.ConditionTrue {
						crossSiteViewFormed.Message = fmt.Sprintf("Cross-Site view: %s", strings.Join(sites, ","))
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
						if err = r.client.Update(context.TODO(), &item); err != nil {
							return nil, err
						}
					}
				}
			}
		}
	}
	return &ispnv1.InfinispanCondition{Type: ispnv1.ConditionCrossSiteViewFormed, Status: metav1.ConditionFalse, Message: "Coordinator not ready"}, nil
}
