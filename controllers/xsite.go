package controllers

import (
	"fmt"
	"strconv"
	"strings"

	ispnv1 "github.com/infinispan/infinispan-operator/api/v1"
	consts "github.com/infinispan/infinispan-operator/controllers/constants"
	ispn "github.com/infinispan/infinispan-operator/pkg/infinispan"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

func (r *infinispanRequest) GetCrossSiteViewCondition(podList *corev1.PodList, siteLocations []string, cluster ispn.ClusterInterface) (*ispnv1.InfinispanCondition, error) {
	for _, item := range podList.Items {
		cacheManagerInfo, err := cluster.GetCacheManagerInfo(consts.DefaultCacheManagerName, item.Name)
		if err == nil {
			if cacheManagerInfo.Coordinator {
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
			}
		}
	}
	return &ispnv1.InfinispanCondition{Type: ispnv1.ConditionCrossSiteViewFormed, Status: metav1.ConditionFalse, Message: "Coordinator not ready"}, nil
}

// GetGossipRouterDeployment returns the deployment for the Gossip Router pod
func (r *infinispanRequest) GetGossipRouterDeployment(m *ispnv1.Infinispan) *appsv1.Deployment {
	lsTunnel := GossipRouterPodLabels(m.Name)
	replicas := int32(1)

	// if the user configures 0 replicas, shutdown the gossip router pod too.
	if m.Spec.Replicas <= 0 {
		replicas = 0
	}

	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.GetGossipRouterDeploymentName(),
			Namespace: m.Namespace,
			Labels:    lsTunnel,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: lsTunnel,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:      m.ObjectMeta.Name,
					Namespace: m.ObjectMeta.Namespace,
					Labels:    lsTunnel,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:    "gossiprouter",
						Image:   m.ImageName(),
						Command: []string{"/opt/gossiprouter/bin/launch.sh"},
						Args:    []string{"-port", strconv.Itoa(consts.CrossSitePort), "-dump_msgs", "registration"},
						Ports: []corev1.ContainerPort{
							{
								ContainerPort: consts.CrossSitePort,
								Name:          "tunnel",
								Protocol:      corev1.ProtocolTCP,
							},
						},
						LivenessProbe:  GossipRouterLivenessProbe(),
						ReadinessProbe: GossipRouterLivenessProbe(),
						StartupProbe:   GossipRouterStartupProbe(),
					}},
				},
			},
			Replicas: pointer.Int32Ptr(replicas),
		},
	}
	return deployment
}
